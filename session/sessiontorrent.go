package session

import (
	"time"

	"github.com/boltdb/bolt"
	"github.com/nictuku/dht"
)

type Torrent struct {
	id           string
	createdAt    time.Time
	port         uint16
	dhtAnnouncer *dhtAnnouncer
	session      *Session
	torrent      *torrent
	removed      chan struct{}
}

func (t *Torrent) ID() string {
	return t.id
}

func (t *Torrent) Name() string {
	return t.torrent.Name()
}

func (t *Torrent) InfoHash() string {
	return t.torrent.InfoHash()
}

func (t *Torrent) CreatedAt() time.Time {
	return t.createdAt
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
	subBucket := t.id
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(subBucket))
		return b.Put([]byte("started"), []byte("1"))
	})
	if err != nil {
		return err
	}
	t.torrent.Start()
	if t.session.config.DHTEnabled && !t.torrent.Stats().Private {
		t.session.mPeerRequests.Lock()
		t.session.dhtPeerRequests[dht.InfoHash(t.torrent.InfoHashBytes())] = struct{}{}
		t.session.mPeerRequests.Unlock()
	}
	return nil
}

func (t *Torrent) Stop() error {
	subBucket := t.id
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(subBucket))
		return b.Put([]byte("started"), []byte("0"))
	})
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}
