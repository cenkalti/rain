// Package torrent provides a BitTorrent client implementation.
package torrent

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/piececache"
	"github.com/cenkalti/rain/internal/resourcemanager"
	"github.com/cenkalti/rain/internal/resumer"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/storage/filestorage"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/trackermanager"
	"github.com/cenkalti/rain/internal/webseedsource"
	"github.com/gofrs/uuid"
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
	dhtPeerRequests map[*torrent]struct{} // key is torrent ID

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

func (s *Session) updateStatsLoop() {
	ticker := time.NewTicker(s.config.StatsWriteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.updateStats()
		case <-s.closeC:
			return
		}
	}
}

func (s *Session) updateStats() {
	err := s.db.Update(func(tx *bolt.Tx) error {
		mb := tx.Bucket(torrentsBucket)
		s.mTorrents.RLock()
		for _, t := range s.torrents {
			b := mb.Bucket([]byte(t.torrent.id))
			_ = b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesDownloaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesUploaded), 10)))
			_ = b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(t.torrent.counters.Read(counterBytesWasted), 10)))
			_ = b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(t.torrent.counters.Read(counterSeededFor)).String()))
		}
		s.mTorrents.RUnlock()
		return nil
	})
	if err != nil {
		s.log.Errorln("cannot update stats:", err.Error())
	}
}

func (s *Session) processDHTResults() {
	dhtLimiter := time.NewTicker(time.Second)
	defer dhtLimiter.Stop()
	for {
		select {
		case <-dhtLimiter.C:
			s.handleDHTtick()
		case res := <-s.dht.PeersRequestResults:
			for ih, peers := range res {
				torrents, ok := s.torrentsByInfoHash[ih]
				if !ok {
					continue
				}
				addrs := parseDHTPeers(peers)
				for _, t := range torrents {
					select {
					case t.torrent.dhtPeersC <- addrs:
					case <-t.removed:
					default:
					}
				}
			}
		case <-s.closeC:
			return
		}
	}
}

func (s *Session) handleDHTtick() {
	s.mPeerRequests.Lock()
	defer s.mPeerRequests.Unlock()
	for t := range s.dhtPeerRequests {
		s.dht.PeersRequestPort(string(t.infoHash[:]), true, t.port)
		delete(s.dhtPeerRequests, t)
		return
	}
}

func parseDHTPeers(peers []string) []*net.TCPAddr {
	addrs := make([]*net.TCPAddr, 0, len(peers))
	for _, peer := range peers {
		if len(peer) != 6 {
			// only IPv4 is supported for now
			continue
		}
		addr := &net.TCPAddr{
			IP:   net.IP(peer[:4]),
			Port: int((uint16(peer[4]) << 8) | uint16(peer[5])),
		}
		addrs = append(addrs, addr)
	}
	return addrs
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

func (s *Session) loadExistingTorrents(ids []string) {
	var loaded int
	var started []*Torrent
	for _, id := range ids {
		res, err := boltdbresumer.New(s.db, torrentsBucket, []byte(id))
		if err != nil {
			s.log.Error(err)
			continue
		}
		hasStarted, err := s.hasStarted(id)
		if err != nil {
			s.log.Error(err)
			continue
		}
		spec, err := res.Read()
		if err != nil {
			s.log.Error(err)
			continue
		}
		var info *metainfo.Info
		var bf *bitfield.Bitfield
		if len(spec.Info) > 0 {
			info2, err2 := metainfo.NewInfo(spec.Info)
			if err2 != nil {
				s.log.Error(err2)
				continue
			}
			info = info2
			if len(spec.Bitfield) > 0 {
				bf3, err3 := bitfield.NewBytes(spec.Bitfield, info.NumPieces)
				if err3 != nil {
					s.log.Error(err3)
					continue
				}
				bf = bf3
			}
		}
		sto, err := filestorage.New(spec.Dest)
		if err != nil {
			s.log.Error(err)
			continue
		}
		t, err := newTorrent2(
			s,
			id,
			spec.InfoHash,
			sto,
			spec.Name,
			spec.Port,
			s.parseTrackers(spec.Trackers),
			res,
			info,
			bf,
			resumer.Stats{
				BytesDownloaded: spec.BytesDownloaded,
				BytesUploaded:   spec.BytesUploaded,
				BytesWasted:     spec.BytesWasted,
				SeededFor:       int64(spec.SeededFor),
			},
		)
		if err != nil {
			s.log.Error(err)
			continue
		}
		t.webseedClient = &s.webseedClient
		t.webseedSources = webseedsource.NewList(spec.URLList)
		go s.checkTorrent(t)
		delete(s.availablePorts, spec.Port)

		t2 := s.newTorrent(t, spec.AddedAt)
		s.log.Debugf("loaded existing torrent: #%d %s", id, t.Name())
		loaded++
		if hasStarted {
			started = append(started, t2)
		}
	}
	s.log.Infof("loaded %d existing torrents", loaded)
	for _, t := range started {
		t.torrent.Start()
	}
}

func (s *Session) hasStarted(id string) (bool, error) {
	started := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(id))
		val := b.Get([]byte("started"))
		if bytes.Equal(val, []byte("1")) {
			started = true
		}
		return nil
	})
	return started, err
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

func (s *Session) AddTorrent(r io.Reader) (*Torrent, error) {
	t, err := s.addTorrentStopped(r)
	if err != nil {
		return nil, err
	}
	return t, t.Start()
}

func (s *Session) addTorrentStopped(r io.Reader) (*Torrent, error) {
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	id, port, res, sto, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	t, err := newTorrent2(
		s,
		id,
		mi.Info.Hash[:],
		sto,
		mi.Info.Name,
		port,
		s.parseTrackers(mi.AnnounceList),
		res,
		mi.Info,
		nil, // bitfield
		resumer.Stats{},
	)
	if err != nil {
		return nil, err
	}
	t.webseedClient = &s.webseedClient
	t.webseedSources = webseedsource.NewList(mi.URLList)
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash: mi.Info.Hash[:],
		Dest:     sto.Dest(),
		Port:     port,
		Name:     mi.Info.Name,
		Trackers: mi.AnnounceList,
		URLList:  mi.URLList,
		Info:     mi.Info.Bytes,
		AddedAt:  time.Now(),
	}
	err = res.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.newTorrent(t, rspec.AddedAt)
	return t2, nil
}

func (s *Session) AddURI(uri string) (*Torrent, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return s.addURL(uri)
	case "magnet":
		return s.addMagnet(uri)
	default:
		return nil, errors.New("unsupported uri scheme: " + u.Scheme)
	}
}

func (s *Session) addURL(u string) (*Torrent, error) {
	client := http.Client{
		Timeout: s.config.TorrentAddHTTPTimeout,
	}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return s.AddTorrent(resp.Body)
}

func (s *Session) addMagnet(link string) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, err
	}
	id, port, res, sto, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	t, err := newTorrent2(
		s,
		id,
		ma.InfoHash[:],
		sto,
		ma.Name,
		port,
		s.parseTrackers(ma.Trackers),
		res,
		nil, // info
		nil, // bitfield
		resumer.Stats{},
	)
	if err != nil {
		return nil, err
	}
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash: ma.InfoHash[:],
		Dest:     sto.Dest(),
		Port:     port,
		Name:     ma.Name,
		Trackers: ma.Trackers,
		AddedAt:  time.Now(),
	}
	err = res.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.newTorrent(t, rspec.AddedAt)
	return t2, t2.Start()
}

func (s *Session) add() (id string, port int, res resumer.Resumer, sto *filestorage.FileStorage, err error) {
	port, err = s.getPort()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	u1, err := uuid.NewV1()
	if err != nil {
		return
	}
	id = base64.RawURLEncoding.EncodeToString(u1[:])
	res, err = boltdbresumer.New(s.db, torrentsBucket, []byte(id))
	if err != nil {
		return
	}
	dest := filepath.Join(s.config.DataDir, id)
	sto, err = filestorage.New(dest)
	if err != nil {
		return
	}
	return
}

func (s *Session) newTorrent(t *torrent, addedAt time.Time) *Torrent {
	t2 := &Torrent{
		session: s,
		torrent: t,
		addedAt: addedAt,
		removed: make(chan struct{}),
	}
	s.mTorrents.Lock()
	defer s.mTorrents.Unlock()
	s.torrents[t.id] = t2
	ih := dht.InfoHash(t.InfoHash())
	s.torrentsByInfoHash[ih] = append(s.torrentsByInfoHash[ih], t2)
	return t2
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

func (s *Session) Stats() SessionStats {
	s.mTorrents.RLock()
	torrents := len(s.torrents)
	s.mTorrents.RUnlock()

	s.mPorts.RLock()
	ports := len(s.availablePorts)
	s.mPorts.RUnlock()

	s.mBlocklist.RLock()
	blocklistTime := s.blocklistTimestamp
	s.mBlocklist.RUnlock()

	ramStats := s.ram.Stats()

	return SessionStats{
		Torrents:                      torrents,
		AvailablePorts:                ports,
		BlockListRules:                s.blocklist.Len(),
		BlockListLastSuccessfulUpdate: blocklistTime,
		PieceCacheItems:               s.pieceCache.Len(),
		PieceCacheSize:                s.pieceCache.Size(),
		PieceCacheUtilization:         s.pieceCache.Utilization(),
		ReadsPerSecond:                s.pieceCache.LoadsPerSecond(),
		ReadsActive:                   s.pieceCache.LoadsActive(),
		ReadsPending:                  s.pieceCache.LoadsWaiting(),
		ReadBytesPerSecond:            s.pieceCache.LoadedBytesPerSecond(),
		ActivePieceBytes:              ramStats.Used,
		TorrentsPendingRAM:            ramStats.Count,
		Uptime:                        time.Since(s.createdAt),
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

// checkTorrent pings the torrent run loop periodically and crashes the program if a torrent does not respond in
// specified timeout. This is not a good behavior for a production program but it helps to find deadlocks easily,
// at least while developing.
func (s *Session) checkTorrent(t *torrent) {
	const interval = 10 * time.Second
	const timeout = 60 * time.Second
	for {
		select {
		case <-time.After(interval):
			select {
			case t.notifyErrorCommandC <- notifyErrorCommand{errCC: make(chan chan error, 1)}:
			case <-t.closeC:
				return
			case <-time.After(timeout):
				crash("torrent does not responsd")
			}
		case <-t.closeC:
			return
		case <-s.closeC:
			return
		}
	}
}

func crash(msg string) {
	b := make([]byte, 100*1024*1024)
	n := runtime.Stack(b, true)
	b = b[:n]
	os.Stderr.Write(b)
	os.Stderr.WriteString("\n")
	panic(msg)
}
