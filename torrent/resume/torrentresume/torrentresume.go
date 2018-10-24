package torrentresume

import (
	"github.com/cenkalti/rain/client/resumedb"
	"github.com/cenkalti/rain/torrent/resume"
	bolt "go.etcd.io/bbolt"
)

var bucketName = []byte("resume")

type TorrentResume struct {
	db *bolt.DB
	resume.DB
}

var _ resume.DB = (*TorrentResume)(nil)

func New(path string, infoHash []byte) (*TorrentResume, error) {
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
	rdb, err := resumedb.New(db, bucketName, infoHash)
	if err != nil {
		return nil, err
	}
	return &TorrentResume{
		db: db,
		DB: rdb,
	}, nil
}

func (r *TorrentResume) Close() error {
	return r.db.Close()
}
