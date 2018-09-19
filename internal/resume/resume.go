package resume

import (
	bolt "go.etcd.io/bbolt"
)

var (
	bucketName  = []byte("resume")
	bitfieldKey = []byte("bitfield")
)

type Resume struct {
	db *bolt.DB
}

func New(path string) (*Resume, error) {
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(bucketName))
		return err
	})
	return &Resume{
		db: db,
	}, nil
}

func (r *Resume) Close() error {
	return r.db.Close()
}

func (r *Resume) Cleanup(torrentIDs map[string]struct{}) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		b.ForEach(func(k, v []byte) error {
			_, ok := torrentIDs[string(k)]
			if !ok {
				b.Delete(k)
			}
			return nil
		})
		return nil
	})
}

func (r *Resume) WriteBitfield(torrentID []byte, value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		var err error
		b, err = b.CreateBucketIfNotExists(torrentID)
		if err != nil {
			return err
		}
		b.Put(bitfieldKey, value)
		return nil
	})
}

func (r *Resume) ReadBitfield(torrentID []byte) (bitfield []byte, err error) {
	err = r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName).Bucket(torrentID)
		value := b.Get(bitfieldKey)
		bitfield = make([]byte, len(value))
		copy(bitfield, value)
		return nil
	})
	return
}
