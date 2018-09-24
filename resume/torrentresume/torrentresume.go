package torrentresume

import (
	"encoding/json"
	"strconv"

	"github.com/cenkalti/rain/resume"
	bolt "go.etcd.io/bbolt"
)

var (
	bucketName = []byte("resume")

	infoHashKey    = []byte("infohash")
	portKey        = []byte("port")
	nameKey        = []byte("name")
	trackersKey    = []byte("trackers")
	storageTypeKey = []byte("storage_type")
	storageArgsKey = []byte("storage_args")
	infoKey        = []byte("info")
	bitfieldKey    = []byte("bitfield")
)

type TorrentResume struct {
	db *bolt.DB
}

var _ resume.DB = (*TorrentResume)(nil)

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

func (r *TorrentResume) Read() (*resume.Spec, error) {
	var spec *resume.Spec
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

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

func (r *TorrentResume) Write(spec *resume.Spec) error {
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
		b := tx.Bucket(bucketName)
		b.Put(infoHashKey, spec.InfoHash)
		b.Put(portKey, []byte(port))
		b.Put(nameKey, []byte(spec.Name))
		b.Put(storageTypeKey, []byte(spec.StorageType))
		b.Put(storageTypeKey, storageArgs)
		b.Put(trackersKey, trackers)
		b.Put(infoKey, spec.Info)
		b.Put(bitfieldKey, spec.Bitfield)
		return nil
	})
}

func (r *TorrentResume) WriteInfo(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		return b.Put(infoKey, value)
	})
}

func (r *TorrentResume) WriteBitfield(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		return b.Put(bitfieldKey, value)
	})
}
