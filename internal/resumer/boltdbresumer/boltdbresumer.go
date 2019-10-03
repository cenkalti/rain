// Package boltdbresumer provides a Resumer implementation that uses a Bolt database file as storage.
package boltdbresumer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
)

// Keys for the persisten storage.
var Keys = struct {
	InfoHash        []byte
	Port            []byte
	Name            []byte
	Trackers        []byte
	URLList         []byte
	FixedPeers      []byte
	Dest            []byte
	Info            []byte
	Bitfield        []byte
	AddedAt         []byte
	BytesDownloaded []byte
	BytesUploaded   []byte
	BytesWasted     []byte
	SeededFor       []byte
	Started         []byte
}{
	InfoHash:        []byte("info_hash"),
	Port:            []byte("port"),
	Name:            []byte("name"),
	Trackers:        []byte("trackers"),
	URLList:         []byte("url_list"),
	FixedPeers:      []byte("fixed_peers"),
	Dest:            []byte("dest"),
	Info:            []byte("info"),
	Bitfield:        []byte("bitfield"),
	AddedAt:         []byte("added_at"),
	BytesDownloaded: []byte("bytes_downloaded"),
	BytesUploaded:   []byte("bytes_uploaded"),
	BytesWasted:     []byte("bytes_wasted"),
	SeededFor:       []byte("seeded_for"),
	Started:         []byte("started"),
}

// Resumer contains methods for saving/loading resume information of a torrent to a BoltDB database.
type Resumer struct {
	db     *bolt.DB
	bucket []byte
}

// New returns a new Resumer.
func New(db *bolt.DB, bucket []byte) (*Resumer, error) {
	err := db.Update(func(tx *bolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(bucket)
		return err2
	})
	if err != nil {
		return nil, err
	}
	return &Resumer{
		db:     db,
		bucket: bucket,
	}, nil
}

// Write the torrent spec for torrent with `torrentID`.
func (r *Resumer) Write(torrentID string, spec *Spec) error {
	port := strconv.Itoa(spec.Port)
	trackers, err := json.Marshal(spec.Trackers)
	if err != nil {
		return err
	}
	urlList, err := json.Marshal(spec.URLList)
	if err != nil {
		return err
	}
	fixedPeers, err := json.Marshal(spec.FixedPeers)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.Bucket(r.bucket).CreateBucketIfNotExists([]byte(torrentID))
		if err != nil {
			return err
		}
		_ = b.Put(Keys.InfoHash, spec.InfoHash)
		_ = b.Put(Keys.Port, []byte(port))
		_ = b.Put(Keys.Name, []byte(spec.Name))
		_ = b.Put(Keys.Trackers, trackers)
		_ = b.Put(Keys.URLList, urlList)
		_ = b.Put(Keys.FixedPeers, fixedPeers)
		_ = b.Put(Keys.Info, spec.Info)
		_ = b.Put(Keys.Bitfield, spec.Bitfield)
		_ = b.Put(Keys.AddedAt, []byte(spec.AddedAt.Format(time.RFC3339)))
		_ = b.Put(Keys.BytesDownloaded, []byte(strconv.FormatInt(spec.BytesDownloaded, 10)))
		_ = b.Put(Keys.BytesUploaded, []byte(strconv.FormatInt(spec.BytesUploaded, 10)))
		_ = b.Put(Keys.BytesWasted, []byte(strconv.FormatInt(spec.BytesWasted, 10)))
		_ = b.Put(Keys.SeededFor, []byte(spec.SeededFor.String()))
		_ = b.Put(Keys.Started, []byte(strconv.FormatBool(spec.Started)))
		return nil
	})
}

// WriteInfo writes only the info dict of a torrent.
func (r *Resumer) WriteInfo(torrentID string, value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return nil
		}
		return b.Put(Keys.Info, value)
	})
}

// WriteBitfield writes only bitfield of a torrent.
func (r *Resumer) WriteBitfield(torrentID string, value []byte) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return nil
		}
		return b.Put(Keys.Bitfield, value)
	})
}

// WriteStarted writes the start status of a torrent.
func (r *Resumer) WriteStarted(torrentID string, value bool) error {
	return r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return nil
		}
		return b.Put(Keys.Started, []byte(strconv.FormatBool(value)))
	})
}

func (r *Resumer) Read(torrentID string) (*Spec, error) {
	var spec *Spec
	err := r.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return fmt.Errorf("bucket not found: %q", torrentID)
		}

		value := b.Get(Keys.InfoHash)
		if value == nil {
			return fmt.Errorf("key not found: %q", string(Keys.InfoHash))
		}

		spec = new(Spec)
		spec.InfoHash = make([]byte, len(value))
		copy(spec.InfoHash, value)

		var err error
		value = b.Get(Keys.Port)
		spec.Port, err = strconv.Atoi(string(value))
		if err != nil {
			return err
		}

		value = b.Get(Keys.Name)
		if value != nil {
			spec.Name = string(value)
		}

		value = b.Get(Keys.Trackers)
		if value != nil {
			err = json.Unmarshal(value, &spec.Trackers)
			if err != nil {
				// Try to unmarshal old format `[]string`
				trackers := make([]string, 0)
				err = json.Unmarshal(value, &trackers)
				if err != nil {
					return err
				}
				// Migrate to new format `[][]string`
				spec.Trackers = make([][]string, len(trackers))
				for i, t := range trackers {
					spec.Trackers[i] = []string{t}
				}
				// Save in new format
				bt, err := json.Marshal(spec.Trackers)
				if err != nil {
					return err
				}
				err = b.Put(Keys.Trackers, bt)
				if err != nil {
					return err
				}
			}
		}

		value = b.Get(Keys.URLList)
		if value != nil {
			err = json.Unmarshal(value, &spec.URLList)
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.FixedPeers)
		if value != nil {
			err = json.Unmarshal(value, &spec.FixedPeers)
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.Info)
		if value != nil {
			spec.Info = make([]byte, len(value))
			copy(spec.Info, value)
		}

		value = b.Get(Keys.Bitfield)
		if value != nil {
			spec.Bitfield = make([]byte, len(value))
			copy(spec.Bitfield, value)
		}

		value = b.Get(Keys.AddedAt)
		if value != nil {
			spec.AddedAt, err = time.Parse(time.RFC3339, string(value))
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.BytesDownloaded)
		if value != nil {
			spec.BytesDownloaded, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.BytesUploaded)
		if value != nil {
			spec.BytesUploaded, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.BytesWasted)
		if value != nil {
			spec.BytesWasted, err = strconv.ParseInt(string(value), 10, 64)
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.SeededFor)
		if value != nil {
			spec.SeededFor, err = time.ParseDuration(string(value))
			if err != nil {
				return err
			}
		}

		value = b.Get(Keys.Started)
		if value != nil {
			spec.Started, err = strconv.ParseBool(string(value))
			if err != nil {
				return err
			}
		}

		return nil
	})
	return spec, err
}
