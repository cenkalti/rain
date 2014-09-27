package rain

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/bt"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
)

type transfer struct {
	rain      *Rain
	tracker   tracker.Tracker
	torrent   *torrent.Torrent
	pieces    []*Piece
	bitfield  bitfield.BitField
	announceC chan *tracker.AnnounceResponse
	peers     map[bt.PeerID]*Peer // connected peers
	peersM    sync.RWMutex
	stopC     chan struct{} // all goroutines stop when closed
	m         sync.Mutex    // protects map fields
	log       logger.Logger

	// tracker sends available peers to this channel
	peersC chan []*net.TCPAddr
	// pieceManager maintains most recent peers which can be connected to in this channel
	// connecter receives from this channel and connects to new peers
	peerC chan *net.TCPAddr
	// will be closed by main loop when all of the remaining pieces are downloaded
	finished     chan struct{}
	onceFinished sync.Once

	// Upload related fields
	requestC chan *Request
	serveC   chan *Request
}

func (r *Rain) newTransfer(tor *torrent.Torrent, where string) (*transfer, error) {
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	log := logger.New("download " + name)

	trk, err := tracker.New(tor.Announce, r)
	if err != nil {
		return nil, err
	}
	files, checkHash, err := prepareFiles(tor.Info, where)
	if err != nil {
		return nil, err
	}
	pieces := NewPieces(tor.Info, files, blockSize)
	bf := bitfield.New(tor.Info.NumPieces)
	if checkHash {
		r.log.Notice("Doing hash check...")
		for _, p := range pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			bf.SetTo(p.Index, p.OK)
		}
		percentDone := bf.Count() * 100 / bf.Len()
		r.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	return &transfer{
		rain:      r,
		tracker:   trk,
		torrent:   tor,
		pieces:    pieces,
		bitfield:  bf,
		announceC: make(chan *tracker.AnnounceResponse),
		peers:     make(map[bt.PeerID]*Peer),
		stopC:     make(chan struct{}),
		log:       log,
		peersC:    make(chan []*net.TCPAddr),
		peerC:     make(chan *net.TCPAddr),
		finished:  make(chan struct{}),
		requestC:  make(chan *Request),
		serveC:    make(chan *Request),
	}, nil
}

func (t *transfer) InfoHash() bt.InfoHash { return t.torrent.Info.Hash }
func (t *transfer) Finished() chan struct{}     { return t.finished }
func (t *transfer) Downloaded() int64 {
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
func (t *transfer) Uploaded() int64 { return 0 } // TODO
func (t *transfer) Left() int64     { return t.torrent.Info.TotalLength - t.Downloaded() }

func (t *transfer) Run() {
	// Download workers
	if !t.bitfield.All() {
		go t.connecter()
		go t.peerManager()
	}

	// Upload workers
	go t.requestSelector()
	for i := 0; i < uploadSlots; i++ {
		go t.pieceUploader()
	}

	go t.announcer()

	for {
		select {
		case announceResponse := <-t.announceC:
			if announceResponse.Error != nil {
				t.log.Error(announceResponse.Error)
				break
			}
			t.log.Infof("Announce: %d seeder, %d leecher", announceResponse.Seeders, announceResponse.Leechers)
			t.peersC <- announceResponse.Peers
		case <-t.stopC:
			t.log.Notice("Transfer is stopped.")
			return
		}
	}
}

func (t *transfer) announcer() {
	var startEvent tracker.Event
	if t.bitfield.All() {
		startEvent = tracker.Completed
	} else {
		startEvent = tracker.Started
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
