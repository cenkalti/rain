package torrentresume

import (
	"github.com/cenkalti/rain/internal/resume"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketName  = []byte("resume")
	bitfieldKey = []byte("bitfield")
)

type TorrentResume struct {
	db *bolt.DB
}

func New(path string) (*TorrentResume, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		return err
	})
	return &TorrentResume{
		db: db,
	}, nil
}

func (r *TorrentResume) Close() error {
	return r.db.Close()
}

func (r *TorrentResume) Read() (resume.Spec, error) {
	return resume.Spec{}, nil
}

func (r *TorrentResume) Write(spec resume.Spec) error {
	return nil
}

func (r *TorrentResume) WriteInfo(value []byte) error {
	return nil
}

func (r *TorrentResume) WriteBitfield(value []byte) error {
	return nil
}

// func (r *TorrentResume) WriteBitfield(torrentID []byte, value []byte) error {
// 	return r.db.Update(func(tx *bolt.Tx) error {
// 		b := tx.Bucket(bucketName)
// 		var err error
// 		b, err = b.CreateBucketIfNotExists(torrentID)
// 		if err != nil {
// 			return err
// 		}
// 		b.Put(bitfieldKey, value)
// 		return nil
// 	})
// }

// func (r *TorrentResume) ReadBitfield(torrentID []byte) (bitfield []byte, err error) {
// 	err = r.db.View(func(tx *bolt.Tx) error {
// 		b := tx.Bucket(bucketName).Bucket(torrentID)
// 		value := b.Get(bitfieldKey)
// 		bitfield = make([]byte, len(value))
// 		copy(bitfield, value)
// 		return nil
// 	})
// 	return
// }
