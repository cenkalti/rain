// Package client provides a torrent client implementation that is capable of downlaoding multiple torrents in parallel.
package client

import (
	"bytes"
	"errors"
	"io"
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
)

var mainBucket = []byte("torrents")

type Client struct {
	config   Config
	db       *bolt.DB
	log      logger.Logger
	torrents map[uint64]*Torrent
	m        sync.RWMutex
}

// New returns a pointer to new Rain BitTorrent client.
func New(cfg Config) (*Client, error) {
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
	c := &Client{
		config:   cfg,
		db:       db,
		log:      l,
		torrents: make(map[uint64]*Torrent),
	}
	return c, c.loadExistingTorrents(ids)
}

func (c *Client) loadExistingTorrents(ids []uint64) error {
	var started []*Torrent
	for _, id := range ids {
		res, err := boltdbresumer.New(c.db, mainBucket, []byte(strconv.FormatUint(id, 10)))
		if err != nil {
			c.log.Error(err)
			continue
		}
		t, err := torrent.NewResume(res, c.config.Torrent)
		if err != nil {
			c.log.Error(err)
			continue
		}
		t2 := &Torrent{
			client:  c,
			torrent: t,
			id:      id,
		}
		c.torrents[id] = t2
		c.log.Infof("loaded existing torrent: #%d %s", id, t.Name())
		subBucket := strconv.FormatUint(id, 10)
		err = c.db.View(func(tx *bolt.Tx) error {
			b := tx.Bucket(mainBucket).Bucket([]byte(subBucket))
			val := b.Get([]byte("started"))
			if bytes.Equal(val, []byte("1")) {
				started = append(started, t2)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, t := range started {
		t.Start()
	}
	return nil
}

func (c *Client) Close() error {
	c.m.Lock()
	defer c.m.Unlock()
	for _, t := range c.torrents {
		t.torrent.Close()
	}
	c.torrents = nil
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
	var id uint64
	err := c.db.Update(func(tx *bolt.Tx) error {
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

	// TODO get port from config
	t, err := f(6881, sto)
	if err != nil {
		return nil, err
	}

	subBucket := strconv.FormatUint(id, 10)

	res, err := boltdbresumer.New(c.db, mainBucket, []byte(subBucket))
	if err != nil {
		return nil, err
	}
	err = t.SetResume(res)
	if err != nil {
		return nil, err
	}
	err = c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(mainBucket).Bucket([]byte(subBucket))
		return b.Put([]byte("started"), []byte("1"))
	})
	if err != nil {
		return nil, err
	}
	t.Start()

	t2 := &Torrent{
		client:  c,
		torrent: t,
		id:      id,
	}

	c.m.Lock()
	defer c.m.Unlock()
	c.torrents[id] = t2
	return t2, nil
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
	delete(c.torrents, id)
	t.torrent.Close()
	subBucket := strconv.FormatUint(id, 10)
	return c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(mainBucket).DeleteBucket([]byte(subBucket))
	})
}
