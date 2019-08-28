package torrent

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
	"github.com/cenkalti/rain/internal/tracker"
)

// Torrent is created from a torrent file or a magnet link.
type Torrent struct {
	torrent *torrent
}

// InfoHash is the unique value that represents the files in a torrent.
type InfoHash [20]byte

// String encodes info hash in hex as 40 characters.
func (h InfoHash) String() string {
	return hex.EncodeToString(h[:])
}

// ID is a unique identifier in the Session.
func (t *Torrent) ID() string {
	return t.torrent.id
}

// Name of the torrent.
func (t *Torrent) Name() string {
	return t.torrent.Name()
}

// InfoHash returns the hash of the info dictionary of torrent file.
// Two different torrents may have the same info hash.
func (t *Torrent) InfoHash() InfoHash {
	var ih InfoHash
	copy(ih[:], t.torrent.InfoHash())
	return ih
}

// AddedAt returns the time that the torrent is added.
func (t *Torrent) AddedAt() time.Time {
	return t.torrent.addedAt
}

// Stats returns statistics about the torrent.
func (t *Torrent) Stats() Stats {
	return t.torrent.Stats()
}

// Trackers returns the list of trackers of this torrent.
func (t *Torrent) Trackers() []Tracker {
	return t.torrent.Trackers()
}

// Peers returns the list of connected (handshake completed) peers of the torrent.
func (t *Torrent) Peers() []Peer {
	return t.torrent.Peers()
}

// Webseeds returns the list of WebSeed sources in the torrent.
func (t *Torrent) Webseeds() []Webseed {
	return t.torrent.Webseeds()
}

// Port returns the TCP port number that the torrent is listening peers.
func (t *Torrent) Port() int {
	return t.torrent.port
}

// AddPeer adds a new peer to the torrent. Does nothing if torrent is stopped.
func (t *Torrent) AddPeer(addr string) error {
	return t.torrent.addPeerString(addr)
}

// AddTracker adds a new tracker to the torrent.
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

// Start downloading the torrent. If all pieces are completed, starts seeding them.
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

// Stop the torrent. Does not block. After Stop is called, the torrent switches into Stopping state.
// During Stopping state, a stop event sent to trackers with a timeout.
// At most 5 seconds later, the torrent switches into Stopped state.
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

// Announce the torrent to all trackers and DHT. It does not overrides the minimum interval value sent by the trackers or set in Config.
func (t *Torrent) Announce() {
	t.torrent.Announce()
}

// Verify pieces of torrent by reading all of the torrents files from disk.
// After Verify called, the torrent is stopped, then verification starts and the torrent switches into Verifying state.
// The torrent stays stopped after verification finishes.
func (t *Torrent) Verify() error {
	err := t.torrent.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Delete([]byte("bitfield"))
	})
	if err != nil {
		return err
	}
	t.torrent.Verify()
	return nil
}
