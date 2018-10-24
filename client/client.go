package client

import (
	"io"
	"path/filepath"
	"strconv"

	"github.com/cenkalti/rain/client/resumedb"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent"
	"github.com/cenkalti/rain/torrent/storage/filestorage"
	"go.etcd.io/bbolt"
)

var mainBucket = []byte("rain")

type Client struct {
	config   *Config
	db       *bolt.DB
	log      logger.Logger
	torrents map[uint64]*Torrent
}

// New returns a pointer to new Rain BitTorrent client.
func New(c *Config) (*Client, error) {
	if c == nil {
		c = NewConfig()
	}
	db, err := bolt.Open(c.Database, 0666, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(mainBucket)
		return err2
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		config:   c,
		db:       db,
		log:      logger.New("client"),
		torrents: make(map[uint64]*Torrent),
	}, nil
}

func (c *Client) Close() error {
	for _, t := range c.torrents {
		t.torrent.Close()
	}
	c.torrents = nil
	return c.db.Close()
}

func (c *Client) ListTorrents() map[uint64]*Torrent {
	return c.torrents
}

func (c *Client) AddTorrent(r io.Reader) (*Torrent, error) {
	var id uint64
	err := c.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(mainBucket)
		id, _ = bucket.NextSequence()
		return nil
	})
	if err != nil {
		return nil, err
	}

	dest := filepath.Join(c.config.DataDir, strconv.FormatUint(id, 64))
	sto, err := filestorage.New(dest)
	if err != nil {
		return nil, err
	}

	// TODO get port from config
	t, err := torrent.New(r, 6881, sto)
	if err != nil {
		return nil, err
	}

	subBucket := strconv.FormatUint(id, 64)
	if err != nil {
		return nil, err
	}

	res, err := resumedb.New(c.db, mainBucket, []byte(subBucket))
	if err != nil {
		return nil, err
	}
	t.SetResume(res)

	return &Torrent{ID: id, torrent: t}, nil
}

type Torrent struct {
	ID      uint64
	torrent *torrent.Torrent
}
