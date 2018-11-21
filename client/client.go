// Package client provides a BitTorrent client implementation that is capable of downlaoding multiple torrents in parallel.
package client

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/torrent/bitfield"
	"github.com/cenkalti/rain/torrent/magnet"
	"github.com/cenkalti/rain/torrent/metainfo"
	"github.com/cenkalti/rain/torrent/resumer/boltdbresumer"
	"github.com/cenkalti/rain/torrent/storage/filestorage"
	"github.com/mitchellh/go-homedir"
	"github.com/nictuku/dht"
)

var mainBucket = []byte("torrents")

type Client struct {
	config Config
	db     *bolt.DB
	log    logger.Logger
	dht    *dht.DHT
	closeC chan struct{}

	mPeerRequests   sync.Mutex
	dhtPeerRequests map[dht.InfoHash]struct{}

	m                  sync.RWMutex
	torrents           map[uint64]*Torrent
	torrentsByInfoHash map[dht.InfoHash][]*Torrent

	mPorts         sync.Mutex
	availablePorts map[uint16]struct{}
}

// New returns a pointer to new Rain BitTorrent client.
func New(cfg Config) (*Client, error) {
	if cfg.PortBegin >= cfg.PortEnd {
		return nil, errors.New("invalid port range")
	}
	rLimit := syscall.Rlimit{
		Cur: cfg.MaxOpenFiles,
		Max: cfg.MaxOpenFiles,
	}
	err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return nil, err
	}
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
	l := logger.New("client")
	var ids []uint64
	err = db.Update(func(tx *bolt.Tx) error {
		b, err2 := tx.CreateBucketIfNotExists(mainBucket)
		if err2 != nil {
			return err2
		}
		return b.ForEach(func(k, _ []byte) error {
			id, err3 := strconv.ParseUint(string(k), 10, 64)
			if err3 != nil {
				l.Error(err3)
				return nil
			}
			ids = append(ids, id)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	dhtConfig := dht.NewConfig()
	dhtConfig.Address = cfg.DHTAddress
	dhtConfig.Port = int(cfg.DHTPort)
	dhtNode, err := dht.New(dhtConfig)
	if err != nil {
		return nil, err
	}
	err = dhtNode.Start()
	if err != nil {
		return nil, err
	}
	ports := make(map[uint16]struct{})
	for p := cfg.PortBegin; p < cfg.PortEnd; p++ {
		ports[p] = struct{}{}
	}
	c := &Client{
		config:             cfg,
		db:                 db,
		log:                l,
		torrents:           make(map[uint64]*Torrent),
		torrentsByInfoHash: make(map[dht.InfoHash][]*Torrent),
		availablePorts:     ports,
		dht:                dhtNode,
		dhtPeerRequests:    make(map[dht.InfoHash]struct{}),
		closeC:             make(chan struct{}),
	}
	err = c.loadExistingTorrents(ids)
	if err != nil {
		return nil, err
	}
	go c.processDHTResults()
	return c, nil
}

func (c *Client) processDHTResults() {
	dhtLimiter := time.NewTicker(time.Second)
	defer dhtLimiter.Stop()
	for {
		select {
		case <-dhtLimiter.C:
			c.handleDHTtick()
		case res := <-c.dht.PeersRequestResults:
			for ih, peers := range res {
				torrents, ok := c.torrentsByInfoHash[ih]
				if !ok {
					continue
				}
				addrs := parseDHTPeers(peers)
				for _, t := range torrents {
					select {
					case t.dhtAnnouncer.peersC <- addrs:
					case <-t.removed:
					}
				}
			}
		case <-c.closeC:
			return
		}
	}
}

func (c *Client) handleDHTtick() {
	c.mPeerRequests.Lock()
	defer c.mPeerRequests.Unlock()
	if len(c.dhtPeerRequests) == 0 {
		return
	}
	var ih dht.InfoHash
	found := false
	for ih = range c.dhtPeerRequests {
		found = true
		break
	}
	if !found {
		return
	}
	c.dht.PeersRequest(string(ih), true)
	delete(c.dhtPeerRequests, ih)
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

func (c *Client) loadExistingTorrents(ids []uint64) error {
	var loaded int
	var started []*Torrent
	for _, id := range ids {
		res, err := boltdbresumer.New(c.db, mainBucket, []byte(strconv.FormatUint(id, 10)))
		if err != nil {
			c.log.Error(err)
			continue
		}
		hasStarted, err := c.hasStarted(id)
		if err != nil {
			c.log.Error(err)
			continue
		}
		spec, err := res.Read()
		if err != nil {
			c.log.Error(err)
			continue
		}
		opt := torrent.Options{
			Name:     spec.Name,
			Port:     spec.Port,
			Trackers: spec.Trackers,
			Resumer:  res,
			Config:   &c.config.Torrent,
		}
		var private bool
		var ann *dhtAnnouncer
		if len(spec.Info) > 0 {
			info, err2 := metainfo.NewInfo(spec.Info)
			if err2 != nil {
				c.log.Error(err2)
				continue
			}
			opt.Info = info
			private = info.Private == 1
			if len(spec.Bitfield) > 0 {
				bf, err3 := bitfield.NewBytes(spec.Bitfield, info.NumPieces)
				if err3 != nil {
					c.log.Error(err3)
					continue
				}
				opt.Bitfield = bf
			}
		}
		if !private {
			ann = newDHTAnnouncer(c.dht, spec.InfoHash, spec.Port)
			opt.DHT = ann
		}
		sto, err := filestorage.New(spec.Dest)
		if err != nil {
			c.log.Error(err)
			continue
		}
		t, err := opt.NewTorrent(spec.InfoHash, sto)
		if err != nil {
			c.log.Error(err)
			continue
		}
		delete(c.availablePorts, uint16(spec.Port))

		t2 := c.newTorrent(t, id, uint16(spec.Port), ann)
		c.log.Debugf("loaded existing torrent: #%d %s", id, t.Name())
		loaded++
		if hasStarted {
			started = append(started, t2)
		}
	}
	c.log.Infof("loaded %d existing torrents", loaded)
	for _, t := range started {
		t.Start()
	}
	return nil
}

func (c *Client) hasStarted(id uint64) (bool, error) {
	subBucket := strconv.FormatUint(id, 10)
	started := false
	err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(mainBucket).Bucket([]byte(subBucket))
		val := b.Get([]byte("started"))
		if bytes.Equal(val, []byte("1")) {
			started = true
		}
		return nil
	})
	return started, err
}

func (c *Client) Close() error {
	c.dht.Stop()

	var wg sync.WaitGroup
	c.m.Lock()
	wg.Add(len(c.torrents))
	for _, t := range c.torrents {
		go func(t *Torrent) {
			t.torrent.Close()
			wg.Done()
		}(t)
	}
	wg.Wait()
	c.torrents = nil
	c.m.Unlock()

	return c.db.Close()
}

func (c *Client) ListTorrents() []*Torrent {
	c.m.RLock()
	defer c.m.RUnlock()
	torrents := make([]*Torrent, 0, len(c.torrents))
	for _, t := range c.torrents {
		torrents = append(torrents, t)
	}
	return torrents
}

func (c *Client) AddTorrent(r io.Reader) (*Torrent, error) {
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	opt, sto, id, err := c.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			c.releasePort(uint16(opt.Port))
		}
	}()
	opt.Name = mi.Info.Name
	opt.Trackers = mi.GetTrackers()
	opt.Info = mi.Info
	var ann *dhtAnnouncer
	if mi.Info.Private != 1 {
		ann = newDHTAnnouncer(c.dht, mi.Info.Hash[:], opt.Port)
		opt.DHT = ann
	}
	t, err := opt.NewTorrent(mi.Info.Hash[:], sto)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash: t.InfoHashBytes(),
		Dest:     sto.Dest(),
		Port:     opt.Port,
		Name:     opt.Name,
		Trackers: opt.Trackers,
		Info:     opt.Info.Bytes,
		Bitfield: opt.Bitfield.Bytes(),
	}
	err = opt.Resumer.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := c.newTorrent(t, id, uint16(opt.Port), ann)
	return t2, t2.Start()
}

func (c *Client) AddMagnet(link string) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, err
	}
	opt, sto, id, err := c.add()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			c.releasePort(uint16(opt.Port))
		}
	}()
	opt.Name = ma.Name
	opt.Trackers = ma.Trackers
	ann := newDHTAnnouncer(c.dht, ma.InfoHash[:], opt.Port)
	opt.DHT = ann
	t, err := opt.NewTorrent(ma.InfoHash[:], sto)
	if err != nil {
		return nil, err
	}
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
	}
	err = opt.Resumer.(*boltdbresumer.Resumer).Write(rspec)
	if err != nil {
		return nil, err
	}
	t2 := c.newTorrent(t, id, uint16(opt.Port), ann)
	return t2, t2.Start()
}

func (c *Client) add() (*torrent.Options, *filestorage.FileStorage, uint64, error) {
	port, err := c.getPort()
	if err != nil {
		return nil, nil, 0, err
	}
	defer func() {
		if err != nil {
			c.releasePort(port)
		}
	}()
	var id uint64
	err = c.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(mainBucket)
		id, err = bucket.NextSequence()
		return err
	})
	if err != nil {
		return nil, nil, 0, err
	}
	res, err := boltdbresumer.New(c.db, mainBucket, []byte(strconv.FormatUint(id, 10)))
	if err != nil {
		return nil, nil, 0, err
	}
	dest := filepath.Join(c.config.DataDir, strconv.FormatUint(id, 10))
	sto, err := filestorage.New(dest)
	if err != nil {
		return nil, nil, 0, err
	}
	return &torrent.Options{
		Port:    int(port),
		Resumer: res,
		Config:  &c.config.Torrent,
	}, sto, id, nil
}

func (c *Client) newTorrent(t *torrent.Torrent, id uint64, port uint16, ann *dhtAnnouncer) *Torrent {
	t2 := &Torrent{
		client:       c,
		torrent:      t,
		id:           id,
		port:         port,
		dhtAnnouncer: ann,
		removed:      make(chan struct{}),
	}
	c.m.Lock()
	defer c.m.Unlock()
	c.torrents[id] = t2
	ih := dht.InfoHash(t.InfoHashBytes())
	c.torrentsByInfoHash[ih] = append(c.torrentsByInfoHash[ih], t2)
	return t2
}

func (c *Client) getPort() (uint16, error) {
	c.mPorts.Lock()
	defer c.mPorts.Unlock()
	for p := range c.availablePorts {
		delete(c.availablePorts, p)
		return p, nil
	}
	return 0, errors.New("no free port")
}

func (c *Client) releasePort(port uint16) {
	c.mPorts.Lock()
	defer c.mPorts.Unlock()
	c.availablePorts[port] = struct{}{}
}

func (c *Client) GetTorrent(id uint64) *Torrent {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.torrents[id]
}

func (c *Client) RemoveTorrent(id uint64) error {
	c.m.Lock()
	defer c.m.Unlock()
	t, ok := c.torrents[id]
	if !ok {
		return nil
	}
	close(t.removed)
	t.torrent.Close()
	delete(c.torrents, id)
	delete(c.torrentsByInfoHash, dht.InfoHash(t.torrent.InfoHashBytes()))
	c.releasePort(t.port)
	subBucket := strconv.FormatUint(id, 10)
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mainBucket).DeleteBucket([]byte(subBucket))
	})
}
