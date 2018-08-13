package torrent

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/uploader"
)

var (
	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + Version + "-")

	// Version of client. Set during build.
	Version = "0000" // zero means development version
)

// Torrent connect to peers and downloads files from swarm.
type Torrent struct {
	peerID        [20]byte           // unique id per torrent
	metainfo      *metainfo.MetaInfo // parsed torrent file
	data          *torrentdata.Data  // provides access to files on disk
	dest          string             // path of files on disk
	port          int                // listen for peer connections
	sKeyHash      [20]byte           // for encryption, hash of the info hash
	bitfield      *bitfield.Bitfield // keeps track of the pieces we have
	completed     chan struct{}      // closed when all pieces are downloaded
	onceCompleted sync.Once          // for closing completed channel only once
	running       bool               // true after Start() is called
	closed        bool               // true after Close() is called
	m             sync.Mutex         // protects running and closed state
	stopC         chan struct{}      // all goroutines stop when closed
	stopWG        sync.WaitGroup     // for waiting running goroutines
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
	if port <= 0 {
		return nil, errors.New("invalid port number")
	}
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
	data, err := torrentdata.New(m.Info, dest)
	if err != nil {
		return nil, err
	}
	bf, err := data.Verify()
	if err != nil {
		return nil, err
	}
	t := &Torrent{
		peerID:    peerID,
		metainfo:  m,
		data:      data,
		dest:      dest,
		port:      port,
		sKeyHash:  mse.HashSKey(m.Info.Hash[:]),
		bitfield:  bf,
		completed: make(chan struct{}),
		log:       logger.New("download " + logName),
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

// Start listening peer port, accepting incoming peer connections and download missing pieces.
//
// Seeding continues after all files are donwloaded.
func (t *Torrent) Start() {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		panic("torrent is closed")
	}
	if t.running {
		return
	}
	t.stopC = make(chan struct{})

	// get peers from tracker
	an := announcer.New(t.metainfo.Announce, t, t.completed, t.log)
	t.stopWG.Add(1)
	go func() {
		defer t.stopWG.Done()
		go an.Run(t.stopC)
	}()

	// manage peer connections
	pm := peermanager.New(t.port, an, t.peerID, t.metainfo.Info.Hash, t.bitfield, t.log)
	t.stopWG.Add(1)
	go func() {
		defer t.stopWG.Done()
		go pm.Run(t.stopC)
	}()

	// request missing pieces from peers
	do := downloader.New(pm, t.data, t.bitfield)
	t.stopWG.Add(1)
	go func() {
		defer t.stopWG.Done()
		go do.Run(t.stopC)
	}()

	// send requested blocks
	up := uploader.New()
	t.stopWG.Add(1)
	go func() {
		defer t.stopWG.Done()
		go up.Run(t.stopC)
	}()
}

// Stop downloading and uploading, disconnect all peers and close peer port.
func (t *Torrent) Stop() {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		panic("torrent is closed")
	}
	if !t.running {
		return
	}
	close(t.stopC)
	t.stopWG.Wait()
}

// Close this torrent and release all resources.
func (t *Torrent) Close() error {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		return nil
	}
	if t.running {
		t.Stop()
	}
	return t.data.Close()
}

// Port returns the port number that the client is listening.
func (t *Torrent) Port() int {
	return t.port
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
