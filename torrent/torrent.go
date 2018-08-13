package torrent

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
	"github.com/cenkalti/rain/internal/uploader"
)

// TODO move these to config
const (
	// Maximum simultaneous uploads.
	uploadSlotsPerTorrent = 4

	// Request pieces in blocks of this size.
	blockSize = 16 * 1024
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + Version + "-")

	// Version of client. Set during build.
	Version = "0000" // zero means development version

	// ErrClosed is returned when doing and operation on an already closed torrent.
	ErrClosed = errors.New("torrent is closed")
)

// Torrent connect to peers and downloads files from swarm.
type Torrent struct {
	peerID        [20]byte           // unique id per torrent
	metainfo      *metainfo.MetaInfo // parsed torrent file
	data          *torrentdata.Data  // provides access to files on disk
	dest          string             // path of files on disk
	port          int                // listen for peer connections
	listener      *net.TCPListener   // listens port for peer connections
	tracker       tracker.Tracker    // tracker to announce
	sKeyHash      [20]byte           // for encryption, hash of the info hash
	bitfield      *bitfield.Bitfield // keeps track of the pieces we have
	completed     chan struct{}      // closed when all pieces are downloaded
	onceCompleted sync.Once          // for closing completed channel only once
	stopC         chan struct{}      // all goroutines stop when closed
	stopWG        sync.WaitGroup     // for waiting running goroutines
	m             sync.Mutex         // protects all state in this torrent and it's peers
	running       bool               // true after Start() is called
	closed        bool               // true after Close() is called
	peerMessages  chan peer.Message  // messages from peers are sent to this channel for downloader
	log           logger.Logger
}

// New returns a new torrent by reading a metainfo file.
//
// Files are read from disk. If there are existing files, hash check will be done.
//
// Returned torrent is in stopped state.
//
// Close must be called before discarding the torrent.
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
		peerID:       peerID,
		metainfo:     m,
		data:         data,
		dest:         dest,
		port:         port,
		tracker:      trk,
		sKeyHash:     mse.HashSKey(m.Info.Hash[:]),
		bitfield:     bf,
		completed:    make(chan struct{}),
		peerMessages: make(chan peer.Message),
		log:          logger.New("download " + logName),
	}
	t.checkCompletion()
	return t, nil
}

func (t *Torrent) checkCompletion() {
	if t.bitfield.All() {
		t.onceCompleted.Do(func() {
			close(t.completed)
			t.log.Notice("Download completed")
		})
	}
}

func newTracker(trackerURL string) (tracker.Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return httptracker.New(u), nil
	case "udp":
		return udptracker.New(u), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

// Start listening peer port, accepting incoming peer connections and download missing pieces.
//
// Seeding continues after all files are donwloaded.
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
	t.listener, err = net.ListenTCP("tcp4", &net.TCPAddr{Port: t.port})
	if err != nil {
		return err
	}
	t.log.Notice("Listening peers on tcp://" + t.listener.Addr().String())

	t.stopC = make(chan struct{})

	an := announcer.New(t.tracker, t, t.completed, t.log)                                    // get peers from tracker
	pm := peermanager.New(t.listener, an, t.peerID, t.metainfo.Info.Hash, t.bitfield, t.log) // maintains connected peer list
	do := downloader.New(pm, t.data, t.bitfield)                                             // request missing pieces from peers
	up := uploader.New()                                                                     // send requested blocks

	workers{an, pm, do, up}.start(t.stopC, &t.stopWG)

	return nil
}

// Stop downloading and uploading, disconnect all peers and close peer port.
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
	// TODO remove temporary structures
	return nil
}

// Close this torrent and release all resources.
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
	// TODO close peer connections
	// err = t.peerManager.Close()
	// if err != nil {
	// 	result = multierror.Append(result, err)
	// }
	err = t.data.Close()
	if err != nil {
		result = multierror.Append(result, err)
	}
	return result
}

// Port returns the port number that the client is listening.
// If the client does not listen any port, returns 0.
func (t *Torrent) Port() int {
	if t.listener != nil {
		return t.listener.Addr().(*net.TCPAddr).Port
	}
	return 0
}

// BytesCompleted returns the number of bytes downlaoded and passed hash check.
func (t *Torrent) BytesCompleted() int64 {
	t.m.Lock()
	defer t.m.Unlock()
	sum := int64(t.bitfield.Count() * t.metainfo.Info.PieceLength)

	// Last piece usually not in full size.
	lastPiece := len(t.data.Pieces) - 1
	if t.bitfield.Test(uint32(lastPiece)) {
		sum -= int64(t.metainfo.Info.PieceLength)
		sum += int64(t.data.Pieces[lastPiece].Length)
	}
	return sum
}

// PeerID is unique per torrent.
func (t *Torrent) PeerID() [20]byte { return t.peerID }

// InfoHash identifies the torrent file that is being downloaded.
func (t *Torrent) InfoHash() [20]byte { return t.metainfo.Info.Hash }

// CompleteNotify returns a channel that is closed once all pieces are downloaded successfully.
func (t *Torrent) CompleteNotify() chan struct{} { return t.completed }

// BytesDownloaded is the number of bytes downloaded from swarm.
//
// Because some pieces may be downloaded more than once, this number may be greater than BytesCompleted returns.
func (t *Torrent) BytesDownloaded() int64 { return t.BytesCompleted() } // TODO not the same thing

// BytesUploaded is the number of bytes uploaded to the swarm.
func (t *Torrent) BytesUploaded() int64 { return 0 } // TODO count uploaded bytes

// BytesLeft is the number of bytes that is needed to complete all missing pieces.
func (t *Torrent) BytesLeft() int64 { return t.BytesTotal() - t.BytesCompleted() }

// BytesTotal is the number of total bytes of files in torrent.
func (t *Torrent) BytesTotal() int64 { return t.metainfo.Info.TotalLength }
