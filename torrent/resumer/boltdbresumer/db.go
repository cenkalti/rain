package boltdbresumer

import (
	"github.com/cenkalti/rain/torrent/resumer"
	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("resume")

type DB struct {
	db *bolt.DB
	resumer.Resumer
}

var _ resumer.Resumer = (*DB)(nil)

func Open(path string, infoHash []byte) (*DB, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(bucketName)
		return err2
	})
	if err != nil {
		return nil, err
	}
	rdb, err := New(db, bucketName, infoHash)
	if err != nil {
		return nil, err
	}
	return &DB{
		db:      db,
		Resumer: rdb,
	}, nil
}

func (r *DB) Close() error {
	return r.db.Close()
}
