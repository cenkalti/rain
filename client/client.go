package client

import (
	"io"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent"
	"go.etcd.io/bbolt"
)

var mainBucket = []byte("rain")

type Client struct {
	config *Config
	db     *bolt.DB
	log    logger.Logger
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
		_, err := tx.CreateBucketIfNotExists(mainBucket)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		config: c,
		db:     db,
		log:    logger.New("client"),
	}, nil
}

func (c *Client) ListTorrents() []Torrent {
	return nil
}

func (c *Client) AddTorrent(r io.Reader) (*Torrent, error) {
	t, err := torrent.New(r, port, sto)
	if err != nil {
		return nil, err
	}
	t.SetResume(res)
	err = db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(mainBucket)
		id, _ := tx.NextSequence()
		return nil
	})
	if err != nil {
		t.Close()
		return nil, err
	}
	return t, nil
}

type Torrent struct {
	ID int
}

type DB interface {
	Add() (int, error)
	List() ([]Torrent, error)
}
