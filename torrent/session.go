// Package torrent provides a BitTorrent client implementation.
package torrent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/piececache"
	"github.com/cenkalti/rain/internal/resourcemanager"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/trackermanager"
	"github.com/mitchellh/go-homedir"
	"github.com/nictuku/dht"
)

var (
	sessionBucket         = []byte("session")
	torrentsBucket        = []byte("torrents")
	blocklistKey          = []byte("blocklist")
	blocklistTimestampKey = []byte("blocklist-timestamp")
)

// Session contains torrents, DHT node, caches and other data structures shared by multiple torrents.
type Session struct {
	config         Config
	db             *bolt.DB
	log            logger.Logger
	dht            *dht.DHT
	rpc            *rpcServer
	trackerManager *trackermanager.TrackerManager
	ram            *resourcemanager.ResourceManager
	pieceCache     *piececache.Cache
	webseedClient  http.Client
	createdAt      time.Time
	closeC         chan struct{}

	mPeerRequests   sync.Mutex
	dhtPeerRequests map[*torrent]struct{}

	mTorrents          sync.RWMutex
	torrents           map[string]*Torrent
	torrentsByInfoHash map[dht.InfoHash][]*Torrent

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
	var err error
	cfg.Database, err = homedir.Expand(cfg.Database)
	if err != nil {
		return nil, err
	}
	cfg.DataDir, err = homedir.Expand(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	err = os.MkdirAll(filepath.Dir(cfg.Database), 0750)
	if err != nil {
		return nil, err
	}
	l := logger.New("session")
	db, err := bolt.Open(cfg.Database, 0640, &bolt.Options{Timeout: time.Second})
	if err == bolt.ErrTimeout {
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
	err = db.Update(func(tx *bolt.Tx) error {
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
	var dhtNode *dht.DHT
	if cfg.DHTEnabled {
		dhtConfig := dht.NewConfig()
		dhtConfig.Address = cfg.DHTAddress
		dhtConfig.Port = int(cfg.DHTPort)
		dhtConfig.DHTRouters = "router.bittorrent.com:6881,dht.transmissionbt.com:6881,router.utorrent.com:6881,dht.libtorrent.org:25401,dht.aelitis.com:6881"
		dhtConfig.SaveRoutingTable = false
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
	bl := blocklist.New()
	c := &Session{
		config:             cfg,
		db:                 db,
		blocklist:          bl,
		trackerManager:     trackermanager.New(bl),
		log:                l,
		torrents:           make(map[string]*Torrent),
		torrentsByInfoHash: make(map[dht.InfoHash][]*Torrent),
		availablePorts:     ports,
		dht:                dhtNode,
		pieceCache:         piececache.New(cfg.PieceCacheSize, cfg.PieceCacheTTL, cfg.ParallelReads),
		ram:                resourcemanager.New(cfg.MaxActivePieceBytes),
		createdAt:          time.Now(),
		closeC:             make(chan struct{}),
		webseedClient: http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					rctx, cancel := context.WithTimeout(ctx, cfg.WebseedNameResolveTimeout)
					defer cancel()
					ip, port, err := tracker.ResolveHost(rctx, addr, bl)
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
				ResponseHeaderTimeout: cfg.WebseedResponseHeaderTimeout,
			},
		},
	}
	err = c.startBlocklistReloader()
	if err != nil {
		return nil, err
	}
	if cfg.DHTEnabled {
		c.dhtPeerRequests = make(map[*torrent]struct{})
		go c.processDHTResults()
	}
	c.loadExistingTorrents(ids)
	if c.config.RPCEnabled {
		c.rpc = newRPCServer(c)
		err = c.rpc.Start(c.config.RPCHost, c.config.RPCPort)
		if err != nil {
			return nil, err
		}
	}
	go c.updateStatsLoop()
	return c, nil
}

func (s *Session) parseTrackers(trackers []string) []tracker.Tracker {
	ret := make([]tracker.Tracker, 0, len(trackers))
	for _, tr := range trackers {
		t, err := s.trackerManager.Get(tr, s.config.TrackerHTTPTimeout, s.config.TrackerHTTPUserAgent)
		if err != nil {
			s.log.Debugln("cannot parse tracker url:", err)
			continue
		}
		ret = append(ret, t)
	}
	return ret
}

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
	return s.db.Close()
}

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

func (s *Session) GetTorrent(id string) *Torrent {
	s.mTorrents.RLock()
	defer s.mTorrents.RUnlock()
	return s.torrents[id]
}

func (s *Session) RemoveTorrent(id string) error {
	t, err := s.removeTorrentFromClient(id)
	if t != nil {
		go s.stopAndRemoveData(t)
	}
	return err
}

func (s *Session) removeTorrentFromClient(id string) (*Torrent, error) {
	s.mTorrents.Lock()
	defer s.mTorrents.Unlock()
	t, ok := s.torrents[id]
	if !ok {
		return nil, nil
	}
	close(t.removed)
	delete(s.torrents, id)
	delete(s.torrentsByInfoHash, dht.InfoHash(t.torrent.InfoHash()))
	return t, s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(torrentsBucket).DeleteBucket([]byte(id))
	})
}

func (s *Session) stopAndRemoveData(t *Torrent) {
	t.torrent.Close()
	s.releasePort(t.torrent.port)
	dest := t.torrent.storage.(*filestorage.FileStorage).Dest()
	err := os.RemoveAll(dest)
	if err != nil {
		s.log.Errorf("cannot remove torrent data. err: %s dest: %s", err, dest)
	}
}

func (s *Session) StartAll() error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("1"))
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

func (s *Session) StopAll() error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		tb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := tb.Bucket([]byte(t.torrent.id))
			_ = b.Put([]byte("started"), []byte("0"))
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
