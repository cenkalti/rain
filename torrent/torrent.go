package torrent

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/metainfo"
	"github.com/cenkalti/rain/mse"
	"github.com/cenkalti/rain/piece"
	"github.com/cenkalti/rain/tracker"
	"github.com/cenkalti/rain/tracker/httptracker"
	"github.com/cenkalti/rain/tracker/udptracker"
	"github.com/hashicorp/go-multierror"
)

// TODO move these to config
const (
	// Do not connect more than maxPeerPerTorrent peers.
	maxPeerPerTorrent = 60

	// Maximum simultaneous uploads.
	uploadSlotsPerTorrent = 4

	// Request pieces in blocks of this size.
	blockSize = 16 * 1024
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + Version + "-")

	// Version of client. Set when building: "$ go build -ldflags "-X github.com/cenkalti/rain.Version 0001" cmd/rain/rain.go"
	Version = "0000" // zero means development version
)

type Torrent struct {
	peerID   [20]byte
	listener *net.TCPListener
	dest     string
	port     int
	sKeyHash [20]byte
	metainfo *metainfo.MetaInfo
	tracker  tracker.Tracker
	peers    map[[20]byte]*peer // connected peers
	pieces   []*piece.Piece
	bitfield *bitfield.Bitfield
	// announceC chan *tracker.AnnounceResponse
	stopC chan struct{} // all goroutines stop when closed
	m     sync.Mutex    // protects all state related with this transfer and it's peers
	log   logger.Logger

	// // tracker sends available peers to this channel
	// peersC chan []*net.TCPAddr
	// // connecter receives from this channel and connects to new peers
	// peerC chan *net.TCPAddr
	// will be closed by main loop when all of the remaining pieces are downloaded
	completed chan struct{}
	// for closing completed channel only once
	onceCompleted sync.Once
	// // peers send requests to this channel
	// requestC chan *peerRequest
	// // uploader decides which request to serve and sends it to this channel
	// serveC      chan *peerRequest
	peerLimiter chan struct{}
}

func New(r io.Reader, dest string, port int) (*Torrent, error) {
	m, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	logName := m.Info.Name
	if len(logName) > 8 {
		logName = logName[:8]
	}
	t := &Torrent{
		metainfo: m,
		dest:     dest,
		port:     port,
		sKeyHash: mse.HashSKey(m.Info.Hash[:]),
		peers:    make(map[[20]byte]*peer),
		stopC:    make(chan struct{}),
		// announceC:   make(chan *tracker.AnnounceResponse),
		// peersC:      make(chan []*net.TCPAddr),
		// peerC:       make(chan *net.TCPAddr),
		completed: make(chan struct{}),
		// requestC:    make(chan *peerRequest),
		// serveC:      make(chan *peerRequest),
		peerLimiter: make(chan struct{}, maxPeerPerTorrent),
		log:         logger.New("download " + logName),
	}
	copy(t.peerID[:], peerIDPrefix)
	_, err = rand.Read(t.peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	t.tracker, err = newTracker(m.Announce)
	if err != nil {
		return nil, err
	}
	files, checkHash, err := prepareFiles(m.Info, dest)
	if err != nil {
		return nil, err
	}
	t.pieces = piece.NewPieces(m.Info, files)
	t.bitfield = bitfield.New(m.Info.NumPieces)
	var percentDone uint32
	if checkHash {
		t.log.Notice("Doing hash check...")
		for _, p := range t.pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			t.bitfield.SetTo(p.Index, p.OK)
		}
		percentDone = t.bitfield.Count() * 100 / t.bitfield.Len()
		t.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	if percentDone == 100 {
		t.onceCompleted.Do(func() {
			close(t.completed)
			t.log.Notice("Download completed")
		})
	}
	return t, nil
}

func newTracker(trackerURL string) (tracker.Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	l := logger.New("tracker " + trackerURL)
	switch u.Scheme {
	case "http", "https":
		return httptracker.New(u, l), nil
	case "udp":
		return udptracker.New(u, l), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

// Start listening peer port, accepting incoming peer connections and download missing pieces.
func (t *Torrent) Start() error {
	// TODO do not allow start after close is called
	t.m.Lock()
	defer t.m.Unlock()
	var err error
	addr := &net.TCPAddr{Port: t.port}
	t.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	t.log.Notice("Listening peers on tcp://" + t.listener.Addr().String())
	go t.accepter()
	go t.announcer()
	go t.downloader()
	go t.uploader()
	return nil
}

func (t *Torrent) Stop() error {
	// TODO not implemented
	t.m.Lock()
	defer t.m.Unlock()
	return nil
}

// Close peer port, connected peer connections and files.
func (t *Torrent) Close() error {
	// TODO torrent must not be running
	// TODO close only once
	t.m.Lock()
	defer t.m.Unlock()
	var result error
	err := t.listener.Close()
	if err != nil {
		result = multierror.Append(result, err)
	}
	for _, p := range t.peers {
		err = p.Close()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}
	// TODO close files
	return result
}

func (t *Torrent) accepter() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			t.log.Error(err)
			return
		}
		go t.handleConn(conn)
	}
}

func (t *Torrent) handleConn(conn net.Conn) {
	log := logger.New("peer <- " + conn.RemoteAddr().String())
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Error("cannot close conn:", err)
		}
	}()
	select {
	case t.peerLimiter <- struct{}{}:
		defer func() { <-t.peerLimiter }()
	default:
		log.Debugln("peer limit reached, rejecting peer")
		return
	}

	// TODO get this from config
	encryptionForceIncoming := false

	encConn, cipher, extensions, peerID, _, err := btconn.Accept(
		conn,
		func(sKeyHash [20]byte) (sKey []byte) {
			if sKeyHash == t.sKeyHash {
				return t.metainfo.Info.Hash[:]
			}
			return nil
		},
		encryptionForceIncoming,
		func(infoHash [20]byte) bool {
			return infoHash == t.metainfo.Info.Hash
		},
		[8]byte{}, // no extension for now
		t.peerID,
	)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			log.Warning(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	p := t.newPeer(encConn, peerID, log)

	t.m.Lock()
	t.peers[peerID] = p
	t.m.Unlock()
	defer func() {
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	p.Run()
}

// Port returns the port number that the client is listening.
// If the client does not listen any port, returns 0.
func (t *Torrent) Port() int {
	if t.listener != nil {
		return t.listener.Addr().(*net.TCPAddr).Port
	}
	return 0
}

// func (t *Torrent) InfoHash() [20]byte            { return t.info.Hash }

func (t *Torrent) CompleteNotify() chan struct{} { return t.completed }

func (t *Torrent) BytesCompleted() int64 {
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

func (t *Torrent) BytesDownloaded() int64 { return t.BytesCompleted() } // TODO not the same thing
func (t *Torrent) BytesUploaded() int64   { return 0 }                  // TODO count uploaded bytes
func (t *Torrent) BytesLeft() int64       { return t.BytesTotal() - t.BytesCompleted() }
func (t *Torrent) BytesTotal() int64      { return t.metainfo.Info.TotalLength }

// func (t *Torrent) run() {
// 	// Start download workers
// 	if !t.bitfield.All() {
// 		go t.connecter()
// 		go t.peerManager()
// 	}

// 	// Start upload workers
// 	go t.requestSelector()
// 	for i := 0; i < uploadSlotsPerTorrent; i++ {
// 		go t.pieceUploader()
// 	}

// 	go t.announcer()

// 	for {
// 		select {
// 		case announce := <-t.announceC:
// 			if announce.Error != nil {
// 				t.log.Error(announce.Error)
// 				break
// 			}
// 			t.log.Infof("Announce: %d seeder, %d leecher", announce.Seeders, announce.Leechers)
// 			t.peersC <- announce.Peers
// 		case <-t.stopC:
// 			t.log.Notice("Transfer is stopped.")
// 			return
// 		}
// 	}
// }

// func (t *Torrent) announcer() {
// 	var startEvent tracker.Event
// 	if t.bitfield.All() {
// 		startEvent = tracker.EventCompleted
// 	} else {
// 		startEvent = tracker.EventStarted
// 	}
// 	tracker.AnnouncePeriodically(t.tracker, t, t.stopC, startEvent, nil, t.announceC)
// }

func prepareFiles(info *metainfo.Info, where string) (files []*os.File, checkHash bool, err error) {
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
