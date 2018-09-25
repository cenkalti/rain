package torrent

import (
	"bytes"
	"errors"
	"io"
	"sync"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/magnet"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/storage/filestorage"
)

var errInvalidResumeFile = errors.New("invalid resume file (info hashes does not match)")

// Torrent connect to peers and downloads files from swarm.
type Torrent struct {
	// TODO remove mutex, use channels
	m          sync.Mutex // protects running and closed state
	closed     bool       // true after Close() is called
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
	m, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	if res != nil {
		rspec, err2 := res.Read()
		if err2 != nil {
			return nil, err2
		}
		if !bytes.Equal(rspec.InfoHash, m.Info.Hash[:]) {
			return nil, errInvalidResumeFile
		}
		if rspec != nil {
			return loadResumeSpec(rspec)
		}
	}
	spec := &downloader.Spec{
		InfoHash: m.Info.Hash,
		Trackers: m.GetTrackers(),
		Port:     port,
		Storage:  sto,
		Resume:   res,
		Info:     m.Info,
	}
	if res != nil {
		err = writeResume(res, spec, m.Info.Name)
		if err != nil {
			return nil, err
		}
	}
	return newTorrent(spec, m.Info.Name)
}

func DownloadMagnet(magnetLink string, port int, sto storage.Storage, res resume.DB) (*Torrent, error) {
	m, err := magnet.New(magnetLink)
	if err != nil {
		return nil, err
	}
	if res != nil {
		rspec, err2 := res.Read()
		if err2 != nil {
			return nil, err2
		}
		if !bytes.Equal(rspec.InfoHash, m.InfoHash[:]) {
			return nil, errInvalidResumeFile
		}
		if rspec != nil {
			return loadResumeSpec(rspec)
		}
	}
	spec := &downloader.Spec{
		InfoHash: m.InfoHash,
		Trackers: m.Trackers,
		Port:     port,
		Storage:  sto,
		Resume:   res,
	}
	if res != nil {
		err = writeResume(res, spec, m.Name)
		if err != nil {
			return nil, err
		}
	}
	return newTorrent(spec, m.Name)
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
	dspec := &downloader.Spec{
		Port: spec.Port,
	}
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
	return newTorrent(dspec, spec.Name)
}

func writeResume(res resume.DB, dspec *downloader.Spec, name string) error {
	rspec := &resume.Spec{
		InfoHash:    dspec.InfoHash[:],
		Port:        dspec.Port,
		Name:        name,
		Trackers:    dspec.Trackers,
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

func newTorrent(spec *downloader.Spec, name string) (*Torrent, error) {
	logName := name
	if len(logName) > 8 {
		logName = logName[:8]
	}

	l := logger.New("download " + logName)

	d, err := downloader.New(spec, l)
	if err != nil {
		return nil, err
	}
	return &Torrent{
		log:        l,
		downloader: d,
	}, nil
}

// Close this torrent and release all resources.
func (t *Torrent) Close() {
	t.m.Lock()
	if t.closed {
		t.m.Unlock()
		return
	}
	t.closed = true
	t.m.Unlock()

	t.downloader.Close()
}

// NotifyComplete returns a channel that is closed once all pieces are downloaded successfully.
func (t *Torrent) NotifyComplete() <-chan struct{} { return t.downloader.NotifyComplete() }

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
