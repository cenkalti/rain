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
	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/worker"
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/storage/filestorage"
)

var (
	// Version of client. Set during build.
	Version = "0000" // zero means development version

	// http://www.bittorrent.org/beps/bep_0020.html
	peerIDPrefix = []byte("-RN" + Version + "-")
)

// Torrent connect to peers and downloads files from swarm.
type Torrent struct {
	peerID     [20]byte // unique id per torrent
	infoHash   [20]byte
	announce   string
	port       int           // listen for peer connections
	closed     bool          // true after Close() is called
	m          sync.Mutex    // protects running and closed state
	completeC  chan struct{} // downloader closes this channel when all pieces are downloaded
	workers    worker.Workers
	log        logger.Logger
	downloader *downloader.Downloader
}

// DownloadTorrent returns a new torrent by reading a metainfo file.
//
// Files are read from disk. If there are existing files, hash check will be done.
//
// Close must be called before discarding the torrent.
//
// Seeding continues after all files are downloaded.
//
// You should listen NotifyComplete and NotifyError channels after starting the torrent.
func DownloadTorrent(r io.Reader, port int, sto storage.Storage, res resume.DB) (*Torrent, error) {
	if res != nil {
		rspec, err := res.Read()
		if err != nil {
			return nil, err
		}
		if rspec != nil {
			return loadResumeSpec(rspec)
		}
	}
	m, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	spec := &downloader.Spec{
		InfoHash: m.Info.Hash,
		Storage:  sto,
		Resume:   res,
		Info:     m.Info,
	}
	if res != nil {
		err = writeResume(res, spec, port, m.Info.Name, m.Announce)
		if err != nil {
			return nil, err
		}
	}
	return newTorrent(spec, port, m.Info.Name, m.Announce)
}

func DownloadMagnet(magnetLink string, port int, sto storage.Storage, res resume.DB) (*Torrent, error) {
	if res != nil {
		rspec, err := res.Read()
		if err != nil {
			return nil, err
		}
		if rspec != nil {
			return loadResumeSpec(rspec)
		}
	}
	m, err := magnet.New(magnetLink)
	if err != nil {
		return nil, err
	}
	spec := &downloader.Spec{
		InfoHash: m.InfoHash,
		Storage:  sto,
		Resume:   res,
	}
	if res != nil {
		err = writeResume(res, spec, port, m.Name, m.Trackers[0])
		if err != nil {
			return nil, err
		}
	}
	return newTorrent(spec, port, m.Name, m.Trackers[0])
}

func Resume(res resume.DB) (*Torrent, error) {
	spec, err := res.Read()
	if err != nil {
		return nil, err
	}
	if spec == nil {
		return nil, errors.New("no resume info")
	}
	return loadResumeSpec(spec)
}

func loadResumeSpec(spec *resume.Spec) (*Torrent, error) {
	var err error
	dspec := &downloader.Spec{}
	copy(dspec.InfoHash[:], spec.InfoHash)
	if len(spec.Info) > 0 {
		dspec.Info, err = metainfo.NewInfo(spec.Info)
		if err != nil {
			return nil, err
		}
		if len(spec.Bitfield) > 0 {
			dspec.Bitfield = bitfield.New(dspec.Info.NumPieces)
			copy(dspec.Bitfield.Bytes(), spec.Bitfield)
		}
	}
	switch spec.StorageType {
	case filestorage.StorageType:
		dspec.Storage = &filestorage.FileStorage{}
	default:
		return nil, errors.New("unknown storage type: " + spec.StorageType)
	}
	err = dspec.Storage.Load(spec.StorageArgs)
	if err != nil {
		return nil, err
	}
	return newTorrent(dspec, spec.Port, spec.Name, spec.Trackers[0])
}

func writeResume(res resume.DB, dspec *downloader.Spec, port int, name string, tracker string) error {
	rspec := &resume.Spec{
		InfoHash: dspec.InfoHash[:],
		Port:     port,
		Name:     name,
		// TODO save every tracker
		Trackers:    []string{tracker},
		StorageType: dspec.Storage.Type(),
		StorageArgs: dspec.Storage.Args(),
	}
	if dspec.Info != nil {
		rspec.Info = dspec.Info.Bytes
	}
	if dspec.Bitfield != nil {
		rspec.Bitfield = dspec.Bitfield.Bytes()
	}
	return res.Write(rspec)
}

// TODO pass every tracker
func newTorrent(spec *downloader.Spec, port int, name string, tracker string) (*Torrent, error) {
	logName := name
	if len(logName) > 8 {
		logName = logName[:8]
	}

	var peerID [20]byte
	copy(peerID[:], peerIDPrefix)
	_, err := rand.Read(peerID[len(peerIDPrefix):]) // nolint: gosec
	if err != nil {
		return nil, err
	}

	completeC := make(chan struct{})
	l := logger.New("download " + logName)

	t := &Torrent{
		peerID:   peerID,
		infoHash: spec.InfoHash,
		// TODO pass every tracker to downloader
		announce:   tracker,
		port:       port,
		log:        l,
		completeC:  completeC,
		downloader: downloader.New(spec, completeC, l),
	}

	// keep list of peer addresses to connect
	pl := peerlist.New()
	t.workers.Start(pl)

	// get peers from tracker
	an := announcer.New(t.announce, t, t.completeC, pl, t.log)
	t.workers.Start(an)

	// manage peer connections
	pm := peermanager.New(t.port, pl, t.peerID, t.infoHash, t.downloader.NewPeers(), t.log)
	t.workers.Start(pm)

	return t, nil
}

// Close this torrent and release all resources.
func (t *Torrent) Close() error {
	t.m.Lock()
	if t.closed {
		t.m.Unlock()
		return nil
	}
	t.closed = true
	t.m.Unlock()

	t.workers.Stop()
	t.downloader.Close()
	return nil
}

// Port returns the port number that the client is listening.
func (t *Torrent) Port() int {
	return t.port
}

// PeerID is unique per torrent.
func (t *Torrent) PeerID() [20]byte { return t.peerID }

// InfoHash identifies the torrent file that is being downloaded.
func (t *Torrent) InfoHash() [20]byte { return t.infoHash }

// NotifyComplete returns a channel that is closed once all pieces are downloaded successfully.
func (t *Torrent) NotifyComplete() <-chan struct{} { return t.completeC }

// NotifyError returns a new channel for waiting download errors.
//
// When error is sent to the channel, torrent is stopped automatically.
func (t *Torrent) NotifyError() <-chan error { return t.downloader.ErrC() }

type Stats struct {
	// Bytes that are downloaded and passed hash check.
	BytesComplete int64

	// BytesLeft is the number of bytes that is needed to complete all missing pieces.
	BytesIncomplete int64

	// BytesTotal is the number of total bytes of files in torrent.
	//
	// BytesTotal = BytesComplete + BytesIncomplete
	BytesTotal int64

	// BytesDownloaded is the number of bytes downloaded from swarm.
	// Because some pieces may be downloaded more than once, this number may be greater than BytesCompleted returns.
	// BytesDownloaded int64

	// BytesUploaded is the number of bytes uploaded to the swarm.
	// BytesUploaded   int64
}

func (t *Torrent) Stats() *Stats {
	t.m.Lock()
	defer t.m.Unlock()

	if t.closed {
		return nil
	}

	ds := t.downloader.Stats()
	return &Stats{
		BytesComplete:   ds.BytesComplete,
		BytesIncomplete: ds.BytesIncomplete,
		BytesTotal:      ds.BytesTotal,
	}
}

func (t *Torrent) BytesDownloaded() int64 { return t.Stats().BytesComplete } // TODO not the same thing
func (t *Torrent) BytesUploaded() int64   { return 0 }                       // TODO implememnt
func (t *Torrent) BytesLeft() int64       { return t.Stats().BytesIncomplete }
