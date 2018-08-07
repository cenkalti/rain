package rain

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/rain/mse"
)

type Transfer struct {
	client    *Client
	hash      [20]byte
	info      *info
	tracker   tracker
	pieces    []*piece
	bitfield  *bitfield
	announceC chan *announceResponse
	peers     map[[20]byte]*peer // connected peers
	stopC     chan struct{}      // all goroutines stop when closed
	m         sync.Mutex         // protects all state related with this transfer and it's peers
	log       logger

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

func (c *Client) newTransfer(hash [20]byte, tracker string, name string) (*Transfer, error) {
	trk, err := c.newTracker(tracker)
	if err != nil {
		return nil, err
	}
	if len(name) > 8 {
		name = name[:8]
	}
	return &Transfer{
		client:    c,
		hash:      hash,
		tracker:   trk,
		announceC: make(chan *announceResponse),
		peers:     make(map[[20]byte]*peer),
		stopC:     make(chan struct{}),
		peersC:    make(chan []*net.TCPAddr),
		peerC:     make(chan *net.TCPAddr),
		completed: make(chan struct{}),
		requestC:  make(chan *peerRequest),
		serveC:    make(chan *peerRequest),
		log:       newLogger("download " + name),
	}, nil
}

func (c *Client) newTransferTorrent(tor *torrent) (*Transfer, error) {
	t, err := c.newTransfer(tor.Info.Hash, tor.Announce, tor.Info.Name)
	if err != nil {
		return nil, err
	}

	t.info = tor.Info

	files, checkHash, err := prepareFiles(tor.Info, c.config.DownloadDir)
	if err != nil {
		return nil, err
	}
	t.pieces = newPieces(tor.Info, files)
	t.bitfield = newBitfield(tor.Info.NumPieces)
	var percentDone uint32
	if checkHash {
		c.log.Notice("Doing hash check...")
		for _, p := range t.pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			t.bitfield.SetTo(p.Index, p.OK)
		}
		percentDone = t.bitfield.Count() * 100 / t.bitfield.Len()
		c.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	if percentDone == 100 {
		t.onceCompleted.Do(func() {
			close(t.completed)
			t.log.Notice("Download completed")
		})
	}
	return t, nil
}

func (c *Client) newTransferMagnet(m *magnet) (*Transfer, error) {
	if len(m.Trackers) == 0 {
		return nil, errors.New("no tracker in magnet link")
	}
	var name string
	if m.Name != "" {
		name = m.Name
	} else {
		name = hex.EncodeToString(m.InfoHash[:])
	}
	return c.newTransfer(m.InfoHash, m.Trackers[0], name)
}

func (t *Transfer) InfoHash() [sha1.Size]byte     { return t.info.Hash }
func (t *Transfer) CompleteNotify() chan struct{} { return t.completed }
func (t *Transfer) Downloaded() int64 {
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
func (t *Transfer) Uploaded() int64 { return 0 } // TODO
func (t *Transfer) Left() int64     { return t.info.TotalLength - t.Downloaded() }

func (t *Transfer) run() {
	// Start download workers
	if !t.bitfield.All() {
		go t.connecter()
		go t.peerManager()
	}

	// Start upload workers
	go t.requestSelector()
	for i := 0; i < uploadSlots; i++ {
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

func (t *Transfer) announcer() {
	var startEvent trackerEvent
	if t.bitfield.All() {
		startEvent = eventCompleted
	} else {
		startEvent = eventStarted
	}
	announcePeriodically(t.tracker, t, t.stopC, startEvent, nil, t.announceC)
}

func prepareFiles(info *info, where string) (files []*os.File, checkHash bool, err error) {
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
	f, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0640)
	if err != nil {
		return
	}

	defer func() {
		if err != nil {
			f.Close()
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

func (t *Transfer) Start() {
	sKey := mse.HashSKey(t.info.Hash[:])
	t.client.transfersM.Lock()
	t.client.transfers[t.info.Hash] = t
	t.client.transfersSKey[sKey] = t
	t.client.transfersM.Unlock()
	go func() {
		defer func() {
			t.client.transfersM.Lock()
			delete(t.client.transfers, t.info.Hash)
			delete(t.client.transfersSKey, sKey)
			t.client.transfersM.Unlock()
		}()
		t.run()
	}()
}

func (t *Transfer) Stop() { close(t.stopC) }

func (t *Transfer) Remove(deleteFiles bool) error { panic("not implemented") }
