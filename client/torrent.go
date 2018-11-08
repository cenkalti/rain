package client

import (
	"strconv"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/dht"
	"github.com/cenkalti/rain/torrent"
)

type Torrent struct {
	id           uint64
	port         uint16
	dhtAnnouncer *dhtAnnouncer
	client       *Client
	torrent      *torrent.Torrent
	removed      chan struct{}
}

func (t *Torrent) ID() uint64 {
	return t.id
}

func (t *Torrent) Name() string {
	return t.torrent.Name()
}

func (t *Torrent) InfoHash() string {
	return t.torrent.InfoHash()
}

func (t *Torrent) Stats() torrent.Stats {
	return t.torrent.Stats()
}

func (t *Torrent) Port() uint16 {
	return t.port
}

func (t *Torrent) Start() error {
	subBucket := strconv.FormatUint(t.id, 10)
	err := t.client.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(mainBucket).Bucket([]byte(subBucket))
		return b.Put([]byte("started"), []byte("1"))
	})
	if err != nil {
		return err
	}
	t.torrent.Start()
	if !t.torrent.Stats().Private {
		t.client.mPeerRequests.Lock()
		t.client.dhtPeerRequests[dht.InfoHash(t.torrent.InfoHashBytes())] = struct{}{}
		t.client.mPeerRequests.Unlock()
	}
	return nil
}

func (t *Torrent) Stop() error {
	subBucket := strconv.FormatUint(t.id, 10)
	err := t.client.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(mainBucket).Bucket([]byte(subBucket))
		return b.Put([]byte("started"), []byte("0"))
	})
	if err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}

func (t *Torrent) setDHTNode() {
	if !t.torrent.Stats().Private {
		t.dhtAnnouncer = newDHTAnnouncer(t.client.dht, t.torrent.InfoHashBytes(), int(t.port))
		t.torrent.SetDHT(t.dhtAnnouncer)
	}
}
