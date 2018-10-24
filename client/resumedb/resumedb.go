package resumedb

import (
	"encoding/json"
	"strconv"

	"github.com/cenkalti/rain/torrent/resume"
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

type ResumeImpl struct {
	db                    *bolt.DB
	mainBucket, subBucket []byte
}

var _ resume.DB = (*ResumeImpl)(nil)

func New(db *bolt.DB, mainBucket, subBucket []byte) (*ResumeImpl, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(mainBucket)
		return err2
	})
	if err != nil {
		return nil, err
	}
	return &ResumeImpl{
		db:         db,
		mainBucket: mainBucket,
		subBucket:  subBucket,
	}, nil
}

func (r *ResumeImpl) Write(spec *resume.Spec) error {
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

func (r *ResumeImpl) WriteInfo(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		return b.Put(infoKey, value)
	})
}

func (r *ResumeImpl) WriteBitfield(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		return b.Put(bitfieldKey, value)
	})
}

func (r *ResumeImpl) Read() (*resume.Spec, error) {
	var spec *resume.Spec
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		if b == nil {
			return nil
		}

		value := b.Get(infoHashKey)
		if value == nil {
			return nil
		}

		spec = new(resume.Spec)
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
