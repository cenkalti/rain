// Package boltdbresumer provides a Resumer implementation that uses a Bolt database file as storage.
package boltdbresumer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer"
)

var (
	infoHashKey        = []byte("info_hash")
	portKey            = []byte("port")
	nameKey            = []byte("name")
	trackersKey        = []byte("trackers")
	destKey            = []byte("dest")
	infoKey            = []byte("info")
	bitfieldKey        = []byte("bitfield")
	addedAtKey         = []byte("added_at")
	bytesDownloadedKey = []byte("bytes_downloaded")
	bytesUploadedKey   = []byte("bytes_uploaded")
	bytesWastedKey     = []byte("bytes_wasted")
	seededForKey       = []byte("seeded_for")
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

func (r *Resumer) Write(spec *Spec) error {
	port := strconv.Itoa(spec.Port)
	trackers, err := json.Marshal(spec.Trackers)
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
		b.Put(destKey, []byte(spec.Dest))
		b.Put(trackersKey, trackers)
		b.Put(infoKey, spec.Info)
		b.Put(bitfieldKey, spec.Bitfield)
		b.Put(addedAtKey, []byte(spec.AddedAt.Format(time.RFC3339)))
		b.Put(bytesDownloadedKey, []byte(strconv.FormatInt(spec.BytesDownloaded, 10)))
		b.Put(bytesUploadedKey, []byte(strconv.FormatInt(spec.BytesUploaded, 10)))
		b.Put(bytesWastedKey, []byte(strconv.FormatInt(spec.BytesWasted, 10)))
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

func (r *Resumer) WriteStats(s resumer.Stats) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		b.Put(bytesDownloadedKey, []byte(strconv.FormatInt(s.BytesDownloaded, 10)))
		b.Put(bytesUploadedKey, []byte(strconv.FormatInt(s.BytesUploaded, 10)))
		b.Put(bytesWastedKey, []byte(strconv.FormatInt(s.BytesWasted, 10)))
		b.Put(seededForKey, []byte(s.SeededFor.String()))
		return nil
	})
}

func (r *Resumer) Read() (*Spec, error) {
	var spec *Spec
	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.mainBucket).Bucket(r.subBucket)
		if b == nil {
			return fmt.Errorf("bucket not found: %q", string(r.subBucket))
		}

		value := b.Get(infoHashKey)
		if value == nil {
			return fmt.Errorf("key not found: %q", string(infoHashKey))
		}

		spec = new(Spec)
		spec.InfoHash = make([]byte, len(value))
		copy(spec.InfoHash, value)

		var err error
		value = b.Get(portKey)
		spec.Port, err = strconv.Atoi(string(value))
		if err != nil {
			return err
		}

		value = b.Get(nameKey)
		if value != nil {
			spec.Name = string(value)
		}

		value = b.Get(trackersKey)
		if value != nil {
			err = json.Unmarshal(value, &spec.Trackers)
			if err != nil {
				return err
			}
		}

		value = b.Get(destKey)
		spec.Dest = string(value)

		value = b.Get(infoKey)
		if value != nil {
			spec.Info = make([]byte, len(value))
			copy(spec.Info, value)
		}

		value = b.Get(bitfieldKey)
		if value != nil {
			spec.Bitfield = make([]byte, len(value))
			copy(spec.Bitfield, value)
		}

		value = b.Get(addedAtKey)
		if value != nil {
			spec.AddedAt, err = time.Parse(time.RFC3339, string(value))
			if err != nil {
				return err
			}
		}

		value = b.Get(bytesDownloadedKey)
		if value != nil {
			spec.BytesDownloaded, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(bytesUploadedKey)
		if value != nil {
			spec.BytesUploaded, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(bytesWastedKey)
		if value != nil {
			spec.BytesWasted, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(seededForKey)
		if value != nil {
			spec.SeededFor, err = time.ParseDuration(string(value))
			if err != nil {
				return err
			}
		}

		return nil
	})
	return spec, err
}
