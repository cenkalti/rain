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
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/torrent/resumer/boltdbresumer"
	"github.com/cenkalti/rain/torrent/storage"
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
					t.torrent.AddPeers(addrs)
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
		t, spec, err := torrent.NewResume(res, c.config.Torrent)
		if err != nil {
			c.log.Error(err)
			continue
		}
		delete(c.availablePorts, uint16(spec.Port))
		t2 := &Torrent{
			client:  c,
			torrent: t,
			id:      id,
			port:    uint16(spec.Port),
		}
		c.torrents[id] = t2
		ih := dht.InfoHash(t.InfoHashBytes())
		c.torrentsByInfoHash[ih] = append(c.torrentsByInfoHash[ih], t2)
		c.log.Infof("loaded existing torrent: #%d %s", id, t.Name())
		if hasStarted {
			started = append(started, t2)
		}
	}
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

	c.m.Lock()
	for _, t := range c.torrents {
		t.torrent.Close()
	}
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
	return c.add(func(port int, sto storage.Storage) (*torrent.Torrent, error) {
		return torrent.New(r, port, sto, c.config.Torrent)
	})
}

func (c *Client) AddMagnet(link string) (*Torrent, error) {
	return c.add(func(port int, sto storage.Storage) (*torrent.Torrent, error) {
		return torrent.NewMagnet(link, port, sto, c.config.Torrent)
	})
}

func (c *Client) add(f func(port int, sto storage.Storage) (*torrent.Torrent, error)) (*Torrent, error) {
	port, err := c.getPort()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			c.releasePort(port)
		}
	}()

	var id uint64
	err = c.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(mainBucket)
		id, _ = bucket.NextSequence()
		return nil
	})
	if err != nil {
		return nil, err
	}

	dest := filepath.Join(c.config.DataDir, strconv.FormatUint(id, 10))
	sto, err := filestorage.New(dest)
	if err != nil {
		return nil, err
	}

	t, err := f(int(port), sto)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			t.Close()
		}
	}()

	subBucket := strconv.FormatUint(id, 10)
	res, err := boltdbresumer.New(c.db, mainBucket, []byte(subBucket))
	if err != nil {
		return nil, err
	}
	_, err = t.SetResume(res)
	if err != nil {
		return nil, err
	}

	t2 := &Torrent{
		client:  c,
		torrent: t,
		id:      id,
		port:    port,
	}
	err = t2.Start()
	if err != nil {
		return nil, err
	}

	c.m.Lock()
	defer c.m.Unlock()
	c.torrents[id] = t2
	ih := dht.InfoHash(t.InfoHashBytes())
	c.torrentsByInfoHash[ih] = append(c.torrentsByInfoHash[ih], t2)
	return t2, nil
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
	t.torrent.Close()
	delete(c.torrents, id)
	delete(c.torrentsByInfoHash, dht.InfoHash(t.torrent.InfoHashBytes()))
	c.releasePort(t.port)
	subBucket := strconv.FormatUint(id, 10)
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mainBucket).DeleteBucket([]byte(subBucket))
	})
}
