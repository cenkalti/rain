package torrent

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/metainfo"
	"github.com/cenkalti/rain/mse"
	"github.com/cenkalti/rain/torrentdata"
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

	ErrClosed = errors.New("torrent is closed")
)

type Torrent struct {
	peerID        [20]byte           // unique id per torrent
	metainfo      *metainfo.MetaInfo // parsed torrent file
	data          *torrentdata.Data  // provides access to files on disk
	dest          string             // path of files on disk
	port          int                // listen for peer connections
	listener      *net.TCPListener   // listens port for peer connections
	tracker       tracker.Tracker    // tracker to announce
	peers         map[[20]byte]*peer // connected peers
	sKeyHash      [20]byte           // for encryption, hash of the info hash
	bitfield      *bitfield.Bitfield // keeps track of the pieces we have
	peerLimiter   chan struct{}      // semaphore for limiting number of peers
	completed     chan struct{}      // closed when all pieces are downloaded
	onceCompleted sync.Once          // for closing completed channel only once
	stopC         chan struct{}      // all goroutines stop when closed
	stopWG        sync.WaitGroup     // for waiting running goroutines
	m             sync.Mutex         // protects all state in this torrent and it's peers
	running       bool               // true after Start() is called
	closed        bool               // true after Close() is called
	log           logger.Logger
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
	var peerID [20]byte
	copy(peerID[:], peerIDPrefix)
	_, err = rand.Read(peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}
	trk, err := newTracker(m.Announce)
	if err != nil {
		return nil, err
	}
	data, err := torrentdata.New(m.Info, dest)
	if err != nil {
		return nil, err
	}
	bf, err := data.Verify()
	if err != nil {
		return nil, err
	}
	t := &Torrent{
		peerID:      peerID,
		metainfo:    m,
		data:        data,
		dest:        dest,
		port:        port,
		tracker:     trk,
		peers:       make(map[[20]byte]*peer),
		sKeyHash:    mse.HashSKey(m.Info.Hash[:]),
		bitfield:    bf,
		peerLimiter: make(chan struct{}, maxPeerPerTorrent),
		completed:   make(chan struct{}),
		log:         logger.New("download " + logName),
	}
	if bf.All() {
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
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		return ErrClosed
	}
	if t.running {
		return nil
	}
	var err error
	addr := &net.TCPAddr{Port: t.port}
	t.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	t.log.Notice("Listening peers on tcp://" + t.listener.Addr().String())
	t.stopC = make(chan struct{})
	t.stopWG.Add(5)
	go t.announcer()
	go t.accepter()
	go t.dialer()
	go t.downloader()
	go t.uploader()
	return nil
}

func (t *Torrent) Stop() error {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		return ErrClosed
	}
	if !t.running {
		return nil
	}
	err := t.listener.Close()
	if err != nil {
		return err
	}
	close(t.stopC)
	t.stopWG.Wait()
	return nil
}

// Close peer port, connected peer connections and files.
func (t *Torrent) Close() error {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		return nil
	}
	if t.running {
		err := t.Stop()
		if err != nil {
			return err
		}
	}
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
	err = t.data.Close()
	if err != nil {
		result = multierror.Append(result, err)
	}
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
	defer t.m.Unlock()
	sum := int64(t.bitfield.Count() * t.metainfo.Info.PieceLength)

	// Last piece usually not in full size.
	lastPiece := t.metainfo.Info.NumPieces - 1
	if t.bitfield.Test(lastPiece) {
		sum -= int64(t.metainfo.Info.PieceLength)
		sum += int64(t.data.Piece(int(lastPiece)).Length)
	}
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
