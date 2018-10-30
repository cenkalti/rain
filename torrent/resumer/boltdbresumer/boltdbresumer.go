// Package boltdbresumer provides a Resumer implementation that uses a Bolt database file as storage.
package boltdbresumer

import (
	"encoding/json"
	"strconv"

	"github.com/cenkalti/rain/torrent/resumer"
	bolt "go.etcd.io/bbolt"
)

var (
	infoHashKey    = []byte("infohash")
	portKey        = []byte("port")
	nameKey        = []byte("name")
	trackersKey    = []byte("trackers")
	storageTypeKey = []byte("storage_type")
	storageArgsKey = []byte("storage_args")
	infoKey        = []byte("info")
	bitfieldKey    = []byte("bitfield")
)

type Resumer struct {
	db                    *bolt.DB
	mainBucket, subBucket []byte
}

var _ resumer.Resumer = (*Resumer)(nil)

func New(db *bolt.DB, mainBucket, subBucket []byte) (*Resumer, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(mainBucket)
		return err2
	})
	if err != nil {
		return nil, err
	}
	return &Resumer{
		db:         db,
		mainBucket: mainBucket,
		subBucket:  subBucket,
	}, nil
}

func (r *Resumer) Write(spec *resumer.Spec) error {
	port := strconv.Itoa(spec.Port)
	trackers, err := json.Marshal(spec.Trackers)
	if err != nil {
		return err
	}
	storageArgs, err := json.Marshal(spec.StorageArgs)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.Bucket(r.mainBucket).CreateBucketIfNotExists(r.subBucket)
		if err != nil {
			return err
		}
		b.Put(infoHashKey, spec.InfoHash)
		b.Put(portKey, []byte(port))
		b.Put(nameKey, []byte(spec.Name))
		b.Put(storageTypeKey, []byte(spec.StorageType))
		b.Put(storageArgsKey, storageArgs)
		b.Put(trackersKey, trackers)
		b.Put(infoKey, spec.Info)
		b.Put(bitfieldKey, spec.Bitfield)
		return nil
	})
}

func (r *Resumer) WriteInfo(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		return b.Put(infoKey, value)
	})
}

func (r *Resumer) WriteBitfield(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		return b.Put(bitfieldKey, value)
	})
}

func (r *Resumer) Read() (*resumer.Spec, error) {
	var spec *resumer.Spec
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		if b == nil {
			return nil
		}

		value := b.Get(infoHashKey)
		if value == nil {
			return nil
		}

		spec = new(resumer.Spec)
		spec.InfoHash = make([]byte, len(value))
		copy(spec.InfoHash, value)

		var err error
		value = b.Get(portKey)
		spec.Port, err = strconv.Atoi(string(value))
		if err != nil {
			return err
		}

		value = b.Get(nameKey)
		spec.Name = string(value)

		value = b.Get(trackersKey)
		err = json.Unmarshal(value, &spec.Trackers)
		if err != nil {
			return err
		}

		value = b.Get(storageTypeKey)
		spec.StorageType = string(value)

		value = b.Get(storageArgsKey)
		err = json.Unmarshal(value, &spec.StorageArgs)
		if err != nil {
			return err
		}

		value = b.Get(infoKey)
		spec.Info = make([]byte, len(value))
		copy(spec.Info, value)

		value = b.Get(bitfieldKey)
		spec.Bitfield = make([]byte, len(value))
		copy(spec.Bitfield, value)

		return nil
	})
	return spec, err
}
