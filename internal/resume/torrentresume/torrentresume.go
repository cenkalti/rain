package torrentresume

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/cenkalti/rain/internal/resume"
	bolt "go.etcd.io/bbolt"
)

var ErrInvalidResumeInfo = errors.New("invalid resume info")

var (
	bucketName = []byte("resume")

	infoHashKey = []byte("infohash")
	portKey     = []byte("port")
	nameKey     = []byte("name")
	destKey     = []byte("dest")
	trackersKey = []byte("trackers")
	infoKey     = []byte("info")
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
	var spec resume.Spec
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)

		value := b.Get(infoHashKey)
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

		value = b.Get(destKey)
		spec.Dest = string(value)

		value = b.Get(trackersKey)
		err = json.Unmarshal(value, &spec.Trackers)
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

func (r *TorrentResume) Write(spec resume.Spec) error {
	port := strconv.Itoa(spec.Port)
	trackers, err := json.Marshal(spec.Trackers)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		b.Put(infoHashKey, spec.InfoHash)
		b.Put(portKey, []byte(port))
		b.Put(nameKey, []byte(spec.Name))
		b.Put(destKey, []byte(spec.Dest))
		b.Put(trackersKey, trackers)
		b.Put(infoKey, spec.Info)
		b.Put(bitfieldKey, spec.Bitfield)
		return nil
	})
}

func (r *TorrentResume) WriteInfo(value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		b.Put(infoKey, value)
		return nil
	})
}

func (r *TorrentResume) WriteBitfield(value []byte) error {
	val := make([]byte, len(value))
	copy(val, value)

	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		println("XXX value", len(value), value)
		// b.Put(bitfieldKey, val)
		b.Put([]byte("foo"), []byte("asdf"))
		return nil
	})
}
