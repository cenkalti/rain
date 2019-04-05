package torrent

import (
	"encoding/hex"
	"net"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/tracker"
)

type Torrent struct {
	session *Session
	torrent *torrent
	addedAt time.Time
	removed chan struct{}
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

func (t *Torrent) Port() int {
	return t.torrent.port
}

func (t *Torrent) AddPeer(addr string) error {
	hoststr, portstr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port64, err := strconv.ParseUint(portstr, 10, 16)
	if err != nil {
		return err
	}
	port := int(port64)
	ip := net.ParseIP(hoststr)
	if ip == nil {
		go t.torrent.resolveAndAddPeer(hoststr, port)
		return nil
	}
	taddr := &net.TCPAddr{
		IP:   ip,
		Port: port,
	}
	t.torrent.AddPeers([]*net.TCPAddr{taddr})
	return nil
}

func (t *Torrent) AddTracker(uri string) error {
	tr, err := t.session.trackerManager.Get(uri, t.session.config.TrackerHTTPTimeout, t.session.getTrackerUserAgent(t.torrent.info.IsPrivate()), int64(t.session.config.TrackerHTTPMaxResponseSize))
	if err != nil {
		return err
	}
	t.torrent.AddTrackers([]tracker.Tracker{tr})
	return nil
}

func (t *Torrent) Start() error {
	err := t.session.db.Update(func(tx *bolt.Tx) error {
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
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Put([]byte("started"), []byte("0"))
	})
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}
