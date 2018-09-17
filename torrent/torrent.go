package torrent

import (
	"crypto/rand"
	"errors"
	"io"
	"sync"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/downloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/worker"
)

var (
	// Version of client. Set during build.
	Version = "0000" // zero means development version

	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + Version + "-")
)

// Torrent connect to peers and downloads files from swarm.
type Torrent struct {
	peerID    [20]byte           // unique id per torrent
	metainfo  *metainfo.MetaInfo // parsed torrent file
	dest      string             // path of files on disk
	port      int                // listen for peer connections
	running   bool               // true after Start() is called
	closed    bool               // true after Close() is called
	m         sync.Mutex         // protects running and closed state
	errC      chan error         // downloader sends critical error to this channel
	completeC chan struct{}      // downloader closes this channel when all pieces are downloaded
	workers   worker.Workers
	log       logger.Logger
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
	return &Torrent{
		peerID:   peerID,
		metainfo: m,
		dest:     dest,
		port:     port,
		log:      logger.New("download " + logName),
	}, nil
}

// Start listening peer port, accepting incoming peer connections and download missing pieces.
//
// Seeding continues after all files are downloaded.
//
// You should listen NotifyComplete and NotifyError channels after starting the torrent.
func (t *Torrent) Start() {
	t.m.Lock()
	defer t.m.Unlock()
	if t.closed {
		panic("torrent is closed")
	}
	if t.running {
		return
	}

	t.errC = make(chan error, 1)
	t.completeC = make(chan struct{})

	// keep list of peer addresses to connect
	pl := peerlist.New()
	t.workers.Start(pl)

	// get peers from tracker
	an := announcer.New(t.metainfo.Announce, t, t.completeC, pl, t.log) // TODO send completed channel
	t.workers.Start(an)

	// manage peer connections
	pm := peermanager.New(t.port, pl, t.peerID, t.metainfo.Info.Hash, t.log)
	t.workers.Start(pm)

	// request missing pieces from peers
	do := downloader.New(t.metainfo.Info.Hash, t.dest, t.metainfo.Info, pm.PeerMessages(), t.completeC, t.errC, t.log)
	t.workers.StartWithOnFinishHandler(do, func() { t.Stop() })
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
	t.workers.Stop()
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
	return nil
}

// Port returns the port number that the client is listening.
func (t *Torrent) Port() int {
	return t.port
}

// BytesCompleted returns the number of bytes downlaoded and passed hash check.
func (t *Torrent) BytesCompleted() int64 {
	return 0
	// TODO implement
	// bf := t.data.Bitfield()
	// sum := int64(bf.Count() * t.metainfo.Info.PieceLength)

	// // Last piece usually not in full size.
	// lastPiece := len(t.data.Pieces) - 1
	// if bf.Test(uint32(lastPiece)) {
	// 	sum -= int64(t.metainfo.Info.PieceLength)
	// 	sum += int64(t.data.Pieces[lastPiece].Length)
	// }
	// return sum
}

// PeerID is unique per torrent.
func (t *Torrent) PeerID() [20]byte { return t.peerID }

// InfoHash identifies the torrent file that is being downloaded.
func (t *Torrent) InfoHash() [20]byte { return t.metainfo.Info.Hash }

// NotifyComplete returns a channel that is closed once all pieces are downloaded successfully.
func (t *Torrent) NotifyComplete() <-chan struct{} { return t.completeC }

// NotifyError returns a new channel for waiting download errors.
//
// When error is sent to the channel, torrent is stopped automatically.
func (t *Torrent) NotifyError() <-chan error { return t.errC }

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
