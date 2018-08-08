package rain

import (
	"crypto/sha1"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/metainfo"
	"github.com/cenkalti/rain/mse"
	"github.com/cenkalti/rain/tracker"
)

type Torrent struct {
	client    *Client
	hash      [20]byte
	dest      string
	info      *torrent.Info
	tracker   tracker.Tracker
	pieces    []*piece
	bitfield  *bitfield.Bitfield
	announceC chan *tracker.AnnounceResponse
	peers     map[[20]byte]*peer // connected peers
	stopC     chan struct{}      // all goroutines stop when closed
	m         sync.Mutex         // protects all state related with this transfer and it's peers
	log       logger.Logger

	// tracker sends available peers to this channel
	peersC chan []*net.TCPAddr
	// connecter receives from this channel and connects to new peers
	peerC chan *net.TCPAddr
	// will be closed by main loop when all of the remaining pieces are downloaded
	completed chan struct{}
	// for closing completed channel only once
	onceCompleted sync.Once
	// peers send requests to this channel
	requestC chan *peerRequest
	// uploader decides which request to serve and sends it to this channel
	serveC chan *peerRequest
}

func (t *Torrent) Start() {
	sKey := mse.HashSKey(t.info.Hash[:])
	t.client.m.Lock()
	t.client.torrents[t.info.Hash] = t
	t.client.torrentsSKey[sKey] = t
	t.client.m.Unlock()
	go func() {
		defer func() {
			t.client.m.Lock()
			delete(t.client.torrents, t.info.Hash)
			delete(t.client.torrentsSKey, sKey)
			t.client.m.Unlock()
		}()
		t.run()
	}()
}

func (t *Torrent) Stop() { close(t.stopC) }

func (t *Torrent) Close() error {
	// TODO not implemented
	return nil
}

func (t *Torrent) InfoHash() [sha1.Size]byte     { return t.info.Hash }
func (t *Torrent) CompleteNotify() chan struct{} { return t.completed }
func (t *Torrent) Downloaded() int64 {
	t.m.Lock()
	var sum int64
	for _, p := range t.pieces {
		if p.OK {
			sum += int64(p.Length)
		}
	}
	t.m.Unlock()
	return sum
}
func (t *Torrent) Uploaded() int64 { return 0 } // TODO
func (t *Torrent) Left() int64     { return t.info.TotalLength - t.Downloaded() }

func (t *Torrent) run() {
	// Start download workers
	if !t.bitfield.All() {
		go t.connecter()
		go t.peerManager()
	}

	// Start upload workers
	go t.requestSelector()
	for i := 0; i < uploadSlotsPerTorrent; i++ {
		go t.pieceUploader()
	}

	go t.announcer()

	for {
		select {
		case announce := <-t.announceC:
			if announce.Error != nil {
				t.log.Error(announce.Error)
				break
			}
			t.log.Infof("Announce: %d seeder, %d leecher", announce.Seeders, announce.Leechers)
			t.peersC <- announce.Peers
		case <-t.stopC:
			t.log.Notice("Transfer is stopped.")
			return
		}
	}
}

func (t *Torrent) announcer() {
	var startEvent tracker.Event
	if t.bitfield.All() {
		startEvent = tracker.EventCompleted
	} else {
		startEvent = tracker.EventStarted
	}
	tracker.AnnouncePeriodically(t.tracker, t, t.stopC, startEvent, nil, t.announceC)
}

func prepareFiles(info *torrent.Info, where string) (files []*os.File, checkHash bool, err error) {
	var f *os.File
	var exists bool

	if !info.MultiFile {
		f, exists, err = openOrAllocate(filepath.Join(where, info.Name), info.Length)
		if err != nil {
			return
		}
		if exists {
			checkHash = true
		}
		files = []*os.File{f}
		return
	}

	// Multiple files
	files = make([]*os.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return
		}
		files[i], exists, err = openOrAllocate(path, f.Length)
		if err != nil {
			return
		}
		if exists {
			checkHash = true
		}
	}
	return
}

func openOrAllocate(path string, length int64) (f *os.File, exists bool, err error) {
	f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0640) // nolint: gosec
	if err != nil {
		return
	}

	defer func() {
		if err != nil {
			_ = f.Close()
		}
	}()

	fi, err := f.Stat()
	if err != nil {
		return
	}

	if fi.Size() == 0 && length != 0 {
		if err = f.Truncate(length); err != nil {
			return
		}
		if err = f.Sync(); err != nil {
			return
		}
	} else {
		if fi.Size() != length {
			err = fmt.Errorf("%s expected to be %d bytes but it is %d bytes", path, length, fi.Size())
			return
		}
		exists = true
	}

	return
}
