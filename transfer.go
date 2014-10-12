package rain

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type transfer struct {
	rain      *Client
	tracker   tracker
	torrent   *torrent
	pieces    []*piece
	bitfield  *bitfield
	announceC chan *announceResponse
	peers     map[PeerID]*peer // connected peers
	peersM    sync.RWMutex
	stopC     chan struct{} // all goroutines stop when closed
	m         sync.Mutex    // protects all state related with this transfer and it's peers
	log       logger

	// tracker sends available peers to this channel
	peersC chan []*net.TCPAddr
	// connecter receives from this channel and connects to new peers
	peerC chan *net.TCPAddr
	// will be closed by main loop when all of the remaining pieces are downloaded
	finished chan struct{}
	// for closing finished channel only once
	onceFinished sync.Once
	// peers send requests to this channel
	requestC chan *peerRequest
	// uploader decides which request to serve and sends it to this channel
	serveC chan *peerRequest
}

func (r *Client) newTransfer(tor *torrent, where string) (*transfer, error) {
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	log := newLogger("download " + name)

	trk, err := r.newTracker(tor.Announce)
	if err != nil {
		return nil, err
	}
	files, checkHash, err := prepareFiles(tor.Info, where)
	if err != nil {
		return nil, err
	}
	finished := make(chan struct{})
	pieces := newPieces(tor.Info, files)
	bf := newBitfield(tor.Info.NumPieces)
	var percentDone uint32
	if checkHash {
		r.log.Notice("Doing hash check...")
		for _, p := range pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			bf.SetTo(p.Index, p.OK)
		}
		percentDone = bf.Count() * 100 / bf.Len()
		r.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	t := &transfer{
		rain:      r,
		tracker:   trk,
		torrent:   tor,
		pieces:    pieces,
		bitfield:  bf,
		announceC: make(chan *announceResponse),
		peers:     make(map[PeerID]*peer),
		stopC:     make(chan struct{}),
		log:       log,
		peersC:    make(chan []*net.TCPAddr),
		peerC:     make(chan *net.TCPAddr),
		finished:  finished,
		requestC:  make(chan *peerRequest),
		serveC:    make(chan *peerRequest),
	}
	if percentDone == 100 {
		t.onceFinished.Do(func() {
			close(t.finished)
			t.log.Notice("Download completed")
		})
	}
	return t, nil
}

func (t *transfer) InfoHash() InfoHash      { return t.torrent.Info.Hash }
func (t *transfer) Finished() chan struct{} { return t.finished }
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

func (t *transfer) announcer() {
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
