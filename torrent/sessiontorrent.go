package torrent

import (
	"encoding/hex"
	"net"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/tracker"
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

// String encodes info hash in hex as 40 characters.
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

func (t *Torrent) Webseeds() []Webseed {
	return t.torrent.Webseeds()
}

func (t *Torrent) Port() uint16 {
	return t.port
}

func (t *Torrent) AddPeer(addr *net.TCPAddr) {
	if addr == nil {
		panic("nil addr")
	}
	t.torrent.AddPeers([]*net.TCPAddr{addr})
}

func (t *Torrent) AddTracker(uri string) error {
	tr, err := t.session.trackerManager.Get(uri, t.session.config.TrackerHTTPTimeout, t.session.config.TrackerHTTPUserAgent)
	if err != nil {
		return err
	}
	t.torrent.AddTrackers([]tracker.Tracker{tr})
	return nil
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
