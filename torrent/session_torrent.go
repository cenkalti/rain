package torrent

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/tracker"
)

type Torrent struct {
	torrent *torrent
}

type InfoHash [20]byte

// String encodes info hash in hex as 40 characters.
func (h InfoHash) String() string {
	return hex.EncodeToString(h[:])
}

func (t *Torrent) ID() string {
	return t.torrent.id
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
	return t.torrent.addedAt
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

func (t *Torrent) Port() int {
	return t.torrent.port
}

func (t *Torrent) AddPeer(addr string) error {
	return t.torrent.addPeerString(addr)
}

func (t *Torrent) AddTracker(uri string) error {
	tr, err := t.torrent.session.trackerManager.Get(uri, t.torrent.session.config.TrackerHTTPTimeout, t.torrent.session.getTrackerUserAgent(t.torrent.info.IsPrivate()), int64(t.torrent.session.config.TrackerHTTPMaxResponseSize))
	if err != nil {
		return err
	}
	err = t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		value := b.Get(boltdbresumer.Keys.Trackers)
		var trackers [][]string
		err = json.Unmarshal(value, &trackers)
		if err != nil {
			return err
		}
		trackers = append(trackers, []string{uri})
		value, err = json.Marshal(trackers)
		if err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.Trackers, value)
	})
	if err != nil {
		return err
	}
	t.torrent.AddTrackers([]tracker.Tracker{tr})
	return nil
}

func (t *Torrent) Start() error {
	err := t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Put([]byte("started"), []byte("1"))
	})
	if err != nil {
		return err
	}
	t.torrent.Start()
	return nil
}

func (t *Torrent) Stop() error {
	err := t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Put([]byte("started"), []byte("0"))
	})
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}
