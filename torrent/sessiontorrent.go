package torrent

import (
	"encoding/hex"
	"time"

	"github.com/boltdb/bolt"
	"github.com/nictuku/dht"
)

type Torrent struct {
	id           string
	addedAt      time.Time
	port         uint16
	dhtAnnouncer *dhtAnnouncer
	session      *Session
	torrent      *torrent
	removed      chan struct{}
}

type InfoHash [20]byte

// String encodes info hash in hex as 40 charachters.
func (h InfoHash) String() string {
	return hex.EncodeToString(h[:])
}

func (t *Torrent) ID() string {
	return t.id
}

func (t *Torrent) Name() string {
	return t.torrent.Name()
}

func (t *Torrent) InfoHash() InfoHash {
	var ih InfoHash
	copy(ih[:], t.torrent.InfoHash())
	return ih
}

func (t *Torrent) AddedAt() time.Time {
	return t.addedAt
}

func (t *Torrent) Stats() Stats {
	return t.torrent.Stats()
}

func (t *Torrent) Trackers() []Tracker {
	return t.torrent.Trackers()
}

func (t *Torrent) Peers() []Peer {
	return t.torrent.Peers()
}

func (t *Torrent) Port() uint16 {
	return t.port
}

func (t *Torrent) Start() error {
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.id))
		return b.Put([]byte("started"), []byte("1"))
	})
	if err != nil {
		return err
	}
	t.torrent.Start()
	if t.session.config.DHTEnabled && !t.torrent.Stats().Private {
		t.session.mPeerRequests.Lock()
		t.session.dhtPeerRequests[dht.InfoHash(t.torrent.InfoHash())] = struct{}{}
		t.session.mPeerRequests.Unlock()
	}
	return nil
}

func (t *Torrent) Stop() error {
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.id))
		return b.Put([]byte("started"), []byte("0"))
	})
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}
