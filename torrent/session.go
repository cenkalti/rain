// Package torrent provides a BitTorrent client implementation.
package torrent

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piececache"
	"github.com/cenkalti/rain/internal/resolver"
	"github.com/cenkalti/rain/internal/resourcemanager"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/trackermanager"
	"github.com/juju/ratelimit"
	"github.com/mitchellh/go-homedir"
	"github.com/nictuku/dht"
	"go.etcd.io/bbolt"
)

var (
	sessionBucket         = []byte("session")
	torrentsBucket        = []byte("torrents")
	blocklistKey          = []byte("blocklist")
	blocklistTimestampKey = []byte("blocklist-timestamp")
	blocklistURLHashKey   = []byte("blocklist-url-hash")
)

// Session contains torrents, DHT node, caches and other data structures shared by multiple torrents.
type Session struct {
	config         Config
	db             *bbolt.DB
	resumer        *boltdbresumer.Resumer
	log            logger.Logger
	extensions     [8]byte
	dht            *dht.DHT
	rpc            *rpcServer
	trackerManager *trackermanager.TrackerManager
	ram            *resourcemanager.ResourceManager[*peer.Peer]
	pieceCache     *piececache.Cache
	webseedClient  http.Client
	createdAt      time.Time
	semWrite       *semaphore.Semaphore
	metrics        *sessionMetrics
	bucketDownload *ratelimit.Bucket
	bucketUpload   *ratelimit.Bucket
	closeC         chan struct{}

	mPeerRequests   sync.Mutex
	dhtPeerRequests map[*torrent]struct{}

	mTorrents          sync.RWMutex
	torrents           map[string]*Torrent
	torrentsByInfoHash map[dht.InfoHash][]*Torrent
	invalidTorrentIDs  []string

	mPorts         sync.RWMutex
	availablePorts map[int]struct{}

	mBlocklist         sync.RWMutex
	blocklist          *blocklist.Blocklist
	blocklistTimestamp time.Time
}

// NewSession creates a new Session for downloading and seeding torrents.
// Returned session must be closed after use.
func NewSession(cfg Config) (*Session, error) {
	if cfg.PortBegin >= cfg.PortEnd {
		return nil, errors.New("invalid port range")
	}
	if cfg.MaxOpenFiles > 0 {
		err := setNoFile(cfg.MaxOpenFiles)
		if err != nil {
			return nil, errors.New("cannot change max open files limit: " + err.Error())
		}
	}

	if cfg.CustomLogHandler != nil {
		logger.SetHandler(cfg.CustomLogHandler)
	}
	if cfg.Debug {
		logger.SetDebug()
	}

	var err error
	cfg.Database, err = homedir.Expand(cfg.Database)
	if err != nil {
		return nil, err
	}
	cfg.DataDir, err = homedir.Expand(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(filepath.Dir(cfg.Database), os.ModeDir|cfg.FilePermissions)
	if err != nil {
		return nil, err
	}
	l := logger.New("session")
	db, err := bbolt.Open(cfg.Database, cfg.FilePermissions&^0111, &bbolt.Options{Timeout: time.Second})
	if err == bbolt.ErrTimeout {
		return nil, errors.New("resume database is locked by another process")
	} else if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()
	var ids []string
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(sessionBucket)
		if err2 != nil {
			return err2
		}
		b, err2 := tx.CreateBucketIfNotExists(torrentsBucket)
		if err2 != nil {
			return err2
		}
		return b.ForEach(func(k, _ []byte) error {
			ids = append(ids, string(k))
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	res, err := boltdbresumer.New(db, torrentsBucket)
	if err != nil {
		return nil, err
	}
	var dhtNode *dht.DHT
	if cfg.DHTEnabled {
		dhtConfig := dht.NewConfig()
		dhtConfig.Address = cfg.DHTHost
		dhtConfig.Port = int(cfg.DHTPort)
		dhtConfig.DHTRouters = strings.Join(cfg.DHTBootstrapNodes, ",")
		dhtConfig.SaveRoutingTable = false
		dhtConfig.NumTargetPeers = 0
		dhtNode, err = dht.New(dhtConfig)
		if err != nil {
			return nil, err
		}
		err = dhtNode.Start()
		if err != nil {
			return nil, err
		}
	}
	ports := make(map[int]struct{})
	for p := cfg.PortBegin; p < cfg.PortEnd; p++ {
		ports[int(p)] = struct{}{}
	}
	bl := blocklist.NewLogger(l.Errorf)
	var blTracker *blocklist.Blocklist
	if cfg.BlocklistEnabledForTrackers {
		blTracker = bl
	}
	c := &Session{
		config:             cfg,
		db:                 db,
		resumer:            res,
		blocklist:          bl,
		trackerManager:     trackermanager.New(blTracker, cfg.DNSResolveTimeout, !cfg.TrackerHTTPVerifyTLS),
		log:                l,
		torrents:           make(map[string]*Torrent),
		torrentsByInfoHash: make(map[dht.InfoHash][]*Torrent),
		availablePorts:     ports,
		dht:                dhtNode,
		pieceCache:         piececache.New(cfg.ReadCacheSize, cfg.ReadCacheTTL, cfg.ParallelReads),
		ram:                resourcemanager.New[*peer.Peer](cfg.WriteCacheSize),
		createdAt:          time.Now(),
		semWrite:           semaphore.New(int(cfg.ParallelWrites)),
		closeC:             make(chan struct{}),
		webseedClient: http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					ip, port, err := resolver.Resolve(ctx, addr, cfg.DNSResolveTimeout, bl)
					if err != nil {
						return nil, err
					}
					var d net.Dialer
					taddr := &net.TCPAddr{IP: ip, Port: port}
					dctx, cancel := context.WithTimeout(ctx, cfg.WebseedDialTimeout)
					defer cancel()
					return d.DialContext(dctx, network, taddr.String())
				},
				TLSHandshakeTimeout:   cfg.WebseedTLSHandshakeTimeout,
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: !cfg.WebseedVerifyTLS}, // nolint: gosec
				ResponseHeaderTimeout: cfg.WebseedResponseHeaderTimeout,
			},
		},
	}
	dlSpeed := cfg.SpeedLimitDownload * 1024
	if cfg.SpeedLimitDownload > 0 {
		c.bucketDownload = ratelimit.NewBucketWithRate(float64(dlSpeed), dlSpeed)
	}
	ulSpeed := cfg.SpeedLimitUpload * 1024
	if cfg.SpeedLimitUpload > 0 {
		c.bucketUpload = ratelimit.NewBucketWithRate(float64(ulSpeed), ulSpeed)
	}
	err = c.startBlocklistReloader()
	if err != nil {
		return nil, err
	}
	ext, err := bitfield.NewBytes(c.extensions[:], 64)
	if err != nil {
		panic(err)
	}
	ext.Set(61) // Fast Extension (BEP 6)
	ext.Set(43) // Extension Protocol (BEP 10)
	if cfg.DHTEnabled {
		ext.Set(63) // DHT Protocol (BEP 5)
		c.dhtPeerRequests = make(map[*torrent]struct{})
	}
	c.initMetrics()
	c.loadExistingTorrents(ids)
	if c.config.RPCEnabled {
		c.rpc = newRPCServer(c)
		err = c.rpc.Start(c.config.RPCHost, c.config.RPCPort)
		if err != nil {
			return nil, err
		}
	}
	if cfg.DHTEnabled {
		go c.processDHTResults()
	}
	go c.updateStatsLoop()
	return c, nil
}

func (s *Session) parseTrackers(tiers [][]string, private bool) []tracker.Tracker {
	ret := make([]tracker.Tracker, 0, len(tiers))
	for _, tier := range tiers {
		trackers := make([]tracker.Tracker, 0, len(tier))
		for _, tr := range tier {
			t, err := s.trackerManager.Get(tr, s.config.TrackerHTTPTimeout, s.getTrackerUserAgent(private), int64(s.config.TrackerHTTPMaxResponseSize))
			if err != nil {
				continue
			}
			trackers = append(trackers, t)
		}
		if len(trackers) > 0 {
			tra := tracker.NewTier(trackers)
			ret = append(ret, tra)
		}
	}
	return ret
}

func (s *Session) getTrackerUserAgent(private bool) string {
	if private {
		return s.config.TrackerHTTPPrivateUserAgent
	}
	return trackerHTTPPublicUserAgent
}

// Close stops all torrents and release the resources.
func (s *Session) Close() error {
	close(s.closeC)

	if s.config.DHTEnabled {
		s.dht.Stop()
	}

	s.updateStats()

	var wg sync.WaitGroup
	s.mTorrents.Lock()
	wg.Add(len(s.torrents))
	for _, t := range s.torrents {
		go func(t *Torrent) {
			t.torrent.Close()
			wg.Done()
		}(t)
	}
	wg.Wait()
	s.torrents = nil
	s.mTorrents.Unlock()

	if s.rpc != nil {
		err := s.rpc.Stop(s.config.RPCShutdownTimeout)
		if err != nil {
			s.log.Errorln("cannot stop RPC server:", err.Error())
		}
	}

	s.ram.Close()
	s.pieceCache.Close()
	s.trackerManager.Close()
	s.metrics.Close()
	return s.db.Close()
}

// ListTorrents returns all torrents in session as a slice.
// The order of the torrents returned is different on each call.
func (s *Session) ListTorrents() []*Torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	return torrents
}

func (s *Session) getPort() (int, error) {
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	for p := range s.availablePorts {
		delete(s.availablePorts, p)
		return p, nil
	}
	return 0, errors.New("no free port")
}

func (s *Session) releasePort(port int) {
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	s.availablePorts[port] = struct{}{}
}

// GetTorrent by its id. Returns nil if torrent with id is not found.
func (s *Session) GetTorrent(id string) *Torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	return s.torrents[id]
}

// RemoveTorrent removes the torrent from the session and delete its files.
func (s *Session) RemoveTorrent(id string) error {
	t, err := s.removeTorrentFromClient(id)
	if t != nil {
		err = s.stopAndRemoveData(t)
	}
	return err
}

func (s *Session) removeTorrentFromClient(id string) (*Torrent, error) {
	s.mTorrents.Lock()
	t, ok := s.torrents[id]
	if !ok {
		s.mTorrents.Unlock()
		return nil, nil
	}
	t.torrent.log.Info("removing torrent")
	delete(s.torrents, id)

	// Delete from the list of torrents with same info hash
	ih := dht.InfoHash(t.torrent.InfoHash())
	a := s.torrentsByInfoHash[ih]
	for i, it := range a {
		if it == t {
			a[i] = a[len(a)-1]
			s.torrentsByInfoHash[ih] = a[:len(a)-1]
			break
		}
	}

	// DHT.RemoveInfoHash below sends a message to DHT loop, hence it is blocking.
	// We need to make sure that we are not holding any lock that cause a block in DHT loop.
	// DHT.PeersRequestResults tries to hold the same lock (mTorrents) when a message is received from
	// DHT.PeersRequestResults. That's why we are releasing the lock before calling DHT.RemoveInfoHash.
	s.mTorrents.Unlock()

	if s.config.DHTEnabled && len(s.torrentsByInfoHash[ih]) == 0 {
		s.dht.RemoveInfoHash(string(ih))
	}
	return t, s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(torrentsBucket).DeleteBucket([]byte(id))
	})
}

func (s *Session) stopAndRemoveData(t *Torrent) error {
	t.torrent.Close()
	s.releasePort(t.torrent.port)
	var err error
	var dest string
	if s.config.DataDirIncludesTorrentID {
		dest = filepath.Join(s.config.DataDir, t.torrent.id)
	} else if t.torrent.info != nil {
		dest = filepath.Join(s.config.DataDir, t.torrent.info.Name)
	}
	if dest != "" {
		err = os.RemoveAll(dest)
		if err != nil {
			s.log.Errorf("cannot remove torrent data. err: %s dest: %s", err, dest)
		}
	}
	return err
}

// StartAll starts all torrents in session.
func (s *Session) StartAll() error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("true"))
		}
		defer s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range s.torrents {
		t.torrent.Start()
	}
	return nil
}

// StopAll stops all torrents in session.
func (s *Session) StopAll() error {
	err := s.db.Update(func(tx *bbolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("false"))
		}
		defer s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		return err
	}
	for _, t := range s.torrents {
		t.torrent.Stop()
	}
	return nil
}

func (s *Session) getDataDir(torrentID string) string {
	if s.config.DataDirIncludesTorrentID {
		return filepath.Join(s.config.DataDir, torrentID)
	}
	return s.config.DataDir
}
