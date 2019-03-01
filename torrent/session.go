// Package torrent provides a BitTorrent client implementation.
package torrent

import (
	"bytes"
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
	"sync/atomic"
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

type Session struct {
	config         Config
	db             *bolt.DB
	log            logger.Logger
	dht            *dht.DHT
	rpc            *rpcServer
	trackerManager *trackermanager.TrackerManager
	ram            *resourcemanager.ResourceManager
	pieceCache     *piececache.Cache
	closeC         chan struct{}

	mPeerRequests   sync.Mutex
	dhtPeerRequests map[dht.InfoHash]struct{}

	m                  sync.RWMutex
	torrents           map[string]*Torrent
	torrentsByInfoHash map[dht.InfoHash][]*Torrent

	mPorts         sync.RWMutex
	availablePorts map[uint16]struct{}

	mBlocklist         sync.RWMutex
	blocklist          *blocklist.Blocklist
	blocklistTimestamp time.Time
}

// New returns a pointer to new Rain BitTorrent client.
func New(cfg Config) (*Session, error) {
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
	ports := make(map[uint16]struct{})
	for p := cfg.PortBegin; p < cfg.PortEnd; p++ {
		ports[p] = struct{}{}
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
		pieceCache:         piececache.New(cfg.PieceCacheSize, cfg.PieceCacheTTL),
		ram:                resourcemanager.New(cfg.MaxActivePieceBytes),
		closeC:             make(chan struct{}),
	}
	err = c.startBlocklistReloader()
	if err != nil {
		return nil, err
	}
	if cfg.DHTEnabled {
		c.dhtPeerRequests = make(map[dht.InfoHash]struct{})
		go c.processDHTResults()
	}
	err = c.loadExistingTorrents(ids)
	if err != nil {
		return nil, err
	}
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
		s.m.RLock()
		for _, t := range s.torrents {
			b := mb.Bucket([]byte(t.id))
			b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(atomic.LoadInt64(&t.torrent.resumerStats.BytesDownloaded), 10)))
			b.Put(boltdbresumer.Keys.BytesDownloaded, []byte(strconv.FormatInt(atomic.LoadInt64(&t.torrent.resumerStats.BytesDownloaded), 10)))
			b.Put(boltdbresumer.Keys.BytesUploaded, []byte(strconv.FormatInt(atomic.LoadInt64(&t.torrent.resumerStats.BytesUploaded), 10)))
			b.Put(boltdbresumer.Keys.BytesWasted, []byte(strconv.FormatInt(atomic.LoadInt64(&t.torrent.resumerStats.BytesWasted), 10)))
			b.Put(boltdbresumer.Keys.SeededFor, []byte(time.Duration(atomic.LoadInt64(&t.torrent.resumerStats.SeededFor)).String()))
		}
		s.m.RUnlock()
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
					case t.dhtAnnouncer.peersC <- addrs:
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
	for ih := range s.dhtPeerRequests {
		s.dht.PeersRequest(string(ih), true)
		delete(s.dhtPeerRequests, ih)
		return
	}
}

func parseDHTPeers(peers []string) []*net.TCPAddr {
	var addrs []*net.TCPAddr
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
	var ret []tracker.Tracker
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

func (s *Session) loadExistingTorrents(ids []string) error {
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
		opt := options{
			Name:       spec.Name,
			Port:       spec.Port,
			Trackers:   s.parseTrackers(spec.Trackers),
			Resumer:    res,
			Blocklist:  s.blocklist,
			PieceCache: s.pieceCache,
			RAM:        s.ram,
			Config:     &s.config,
			Stats: resumer.Stats{
				BytesDownloaded: spec.BytesDownloaded,
				BytesUploaded:   spec.BytesUploaded,
				BytesWasted:     spec.BytesWasted,
				SeededFor:       int64(spec.SeededFor),
			},
		}
		var private bool
		var ann *dhtAnnouncer
		if len(spec.Info) > 0 {
			info, err2 := metainfo.NewInfo(spec.Info)
			if err2 != nil {
				s.log.Error(err2)
				continue
			}
			opt.Info = info
			private = info.Private == 1
			if len(spec.Bitfield) > 0 {
				bf, err3 := bitfield.NewBytes(spec.Bitfield, info.NumPieces)
				if err3 != nil {
					s.log.Error(err3)
					continue
				}
				opt.Bitfield = bf
			}
		}
		if s.config.DHTEnabled && !private {
			ann = newDHTAnnouncer(s.dht, spec.InfoHash, spec.Port)
			opt.DHT = ann
		}
		sto, err := filestorage.New(spec.Dest)
		if err != nil {
			s.log.Error(err)
			continue
		}
		t, err := opt.NewTorrent(spec.InfoHash, sto)
		if err != nil {
			s.log.Error(err)
			continue
		}
		go s.checkTorrent(t)
		delete(s.availablePorts, uint16(spec.Port))

		t2 := s.newTorrent(t, id, uint16(spec.Port), spec.AddedAt, ann)
		s.log.Debugf("loaded existing torrent: #%d %s", id, t.Name())
		loaded++
		if hasStarted {
			started = append(started, t2)
		}
	}
	s.log.Infof("loaded %d existing torrents", loaded)
	for _, t := range started {
		t.Start()
	}
	return nil
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
	s.m.Lock()
	wg.Add(len(s.torrents))
	for _, t := range s.torrents {
		go func(t *Torrent) {
			t.torrent.Close()
			wg.Done()
		}(t)
	}
	wg.Wait()
	s.torrents = nil
	s.m.Unlock()

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
	s.m.RLock()
	defer s.m.RUnlock()
	torrents := make([]*Torrent, 0, len(s.torrents))
	for _, t := range s.torrents {
		torrents = append(torrents, t)
	}
	return torrents
}

func (s *Session) AddTorrent(r io.Reader) (*Torrent, error) {
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	opt, sto, id, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(uint16(opt.Port))
		}
	}()
	opt.Name = mi.Info.Name
	opt.Trackers = s.parseTrackers(mi.GetTrackers())
	opt.Info = mi.Info
	var ann *dhtAnnouncer
	if s.config.DHTEnabled && mi.Info.Private != 1 {
		ann = newDHTAnnouncer(s.dht, mi.Info.Hash[:], opt.Port)
		opt.DHT = ann
	}
	t, err := opt.NewTorrent(mi.Info.Hash[:], sto)
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
		InfoHash: t.InfoHash(),
		Dest:     sto.Dest(),
		Port:     opt.Port,
		Name:     opt.Name,
		Trackers: mi.GetTrackers(),
		Info:     opt.Info.Bytes,
		AddedAt:  time.Now().UTC(),
	}
	if opt.Bitfield != nil {
		rspec.Bitfield = opt.Bitfield.Bytes()
	}
	err = opt.Resumer.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.newTorrent(t, id, uint16(opt.Port), rspec.AddedAt, ann)
	return t2, t2.Start()
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
	opt, sto, id, err := s.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(uint16(opt.Port))
		}
	}()
	opt.Name = ma.Name
	opt.Trackers = s.parseTrackers(ma.Trackers)
	var ann *dhtAnnouncer
	if s.config.DHTEnabled {
		ann = newDHTAnnouncer(s.dht, ma.InfoHash[:], opt.Port)
		opt.DHT = ann
	}
	t, err := opt.NewTorrent(ma.InfoHash[:], sto)
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
		Port:     opt.Port,
		Name:     opt.Name,
		Trackers: ma.Trackers,
		AddedAt:  time.Now().UTC(),
	}
	err = opt.Resumer.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.newTorrent(t, id, uint16(opt.Port), rspec.AddedAt, ann)
	return t2, t2.Start()
}

func (s *Session) add() (*options, *filestorage.FileStorage, string, error) {
	port, err := s.getPort()
	if err != nil {
		return nil, nil, "", err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	u1, err := uuid.NewV1()
	if err != nil {
		return nil, nil, "", err
	}
	id := base64.RawURLEncoding.EncodeToString(u1[:])
	res, err := boltdbresumer.New(s.db, torrentsBucket, []byte(id))
	if err != nil {
		return nil, nil, "", err
	}
	dest := filepath.Join(s.config.DataDir, id)
	sto, err := filestorage.New(dest)
	if err != nil {
		return nil, nil, "", err
	}
	return &options{
		Port:       int(port),
		Resumer:    res,
		Blocklist:  s.blocklist,
		PieceCache: s.pieceCache,
		RAM:        s.ram,
		Config:     &s.config,
	}, sto, id, nil
}

func (s *Session) newTorrent(t *torrent, id string, port uint16, addedAt time.Time, ann *dhtAnnouncer) *Torrent {
	t2 := &Torrent{
		session:      s,
		torrent:      t,
		id:           id,
		port:         port,
		addedAt:      addedAt,
		dhtAnnouncer: ann,
		removed:      make(chan struct{}),
	}
	s.m.Lock()
	defer s.m.Unlock()
	s.torrents[id] = t2
	ih := dht.InfoHash(t.InfoHash())
	s.torrentsByInfoHash[ih] = append(s.torrentsByInfoHash[ih], t2)
	return t2
}

func (s *Session) getPort() (uint16, error) {
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	for p := range s.availablePorts {
		delete(s.availablePorts, p)
		return p, nil
	}
	return 0, errors.New("no free port")
}

func (s *Session) releasePort(port uint16) {
	s.mPorts.Lock()
	defer s.mPorts.Unlock()
	s.availablePorts[port] = struct{}{}
}

func (s *Session) GetTorrent(id string) *Torrent {
	s.m.RLock()
	defer s.m.RUnlock()
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
	s.m.Lock()
	defer s.m.Unlock()
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
	s.releasePort(t.port)
	dest := t.torrent.storage.(*filestorage.FileStorage).Dest()
	err := os.RemoveAll(dest)
	if err != nil {
		s.log.Errorf("cannot remove torrent data. err: %s dest: %s", err, dest)
	}
}

func (s *Session) Stats() SessionStats {
	s.m.RLock()
	torrents := len(s.torrents)
	s.m.RUnlock()

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
		ActivePieceBytes:              ramStats.Used,
		TorrentsPendingRAM:            ramStats.Count,
	}
}

func (s *Session) StartAll() {
	s.m.RLock()
	defer s.m.RUnlock()
	for _, t := range s.torrents {
		t.Start()
	}
}

func (s *Session) StopAll() {
	s.m.RLock()
	defer s.m.RUnlock()
	for _, t := range s.torrents {
		t.Stop()
	}
}

func (s *Session) checkTorrent(t *torrent) {
	const interval = 5 * time.Second
	const timeout = 30 * time.Second
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
	b := make([]byte, 30*1024*1024)
	runtime.Stack(b, true)
	os.Stderr.Write(b)
	os.Stderr.WriteString("\n")
	panic(msg)
}
