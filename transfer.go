package rain

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/peer"
	"github.com/cenkalti/rain/piece"
)

// transfer represents an active transfer in the program.
type transfer struct {
	rain       *Rain
	tracker    tracker.Tracker
	torrent    *torrent.Torrent
	pieces     []*piece.Piece
	bitField   bitfield.BitField // pieces that we have
	Finished   chan struct{}     // downloading finished
	downloader *downloader
	uploader   *uploader
	peers      map[*peer.Peer]struct{}
	peersM     sync.RWMutex
	log        logger.Logger
}

func (r *Rain) newTransfer(tor *torrent.Torrent, where string) (*transfer, error) {
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	log := logger.New("download " + name)

	tracker, err := tracker.New(tor.Announce, r)
	if err != nil {
		return nil, err
	}
	files, checkHash, err := prepareFiles(tor.Info, where)
	if err != nil {
		return nil, err
	}
	pieces := piece.NewPieces(tor.Info, files, blockSize)
	bitField := bitfield.New(uint32(len(pieces)))
	if checkHash {
		r.log.Notice("Doing hash check...")
		for _, p := range pieces {
			ok, err := p.HashCheck()
			if err != nil {
				return nil, err
			}
			if ok {
				bitField.Set(p.Index())
			}
		}
		percentDone := (bitField.Count() * 100) / bitField.Len()
		r.log.Noticef("Already downloaded: %d%%", percentDone)
	}
	t := &transfer{
		rain:     r,
		tracker:  tracker,
		torrent:  tor,
		pieces:   pieces,
		bitField: bitField,
		Finished: make(chan struct{}),
		peers:    make(map[*peer.Peer]struct{}),
		log:      log,
	}
	t.downloader = newDownloader(t)
	t.uploader = newUploader(t)
	return t, nil
}

func (t *transfer) BitField() bitfield.BitField { return t.bitField }
func (t *transfer) PieceLength(i uint32) uint32 { return t.pieces[i].Length() }
func (t *transfer) InfoHash() protocol.InfoHash { return t.torrent.Info.Hash }
func (t *transfer) Downloaded() int64 {
	var sum int64
	for i := uint32(0); i < t.bitField.Len(); i++ {
		if t.bitField.Test(i) {
			sum += int64(t.pieces[i].Length())
		}
	}
	return sum
}
func (t *transfer) Uploaded() int64             { return 0 } // TODO
func (t *transfer) Left() int64                 { return t.torrent.Info.TotalLength - t.Downloaded() }
func (t *transfer) Downloader() peer.Downloader { return t.downloader }
func (t *transfer) Uploader() peer.Uploader     { return t.uploader }

func (t *transfer) Run() {
	announceC := make(chan *tracker.AnnounceResponse)
	if t.bitField.All() {
		go tracker.AnnouncePeriodically(t.tracker, t, nil, tracker.Completed, nil, announceC)
	} else {
		go tracker.AnnouncePeriodically(t.tracker, t, nil, tracker.Started, nil, announceC)
	}

	go t.downloader.Run()
	go t.uploader.Run()

	for {
		select {
		case announceResponse := <-announceC:
			if announceResponse.Error != nil {
				t.log.Error(announceResponse.Error)
				break
			}
			t.log.Infof("Announce: %d seeder, %d leecher", announceResponse.Seeders, announceResponse.Leechers)
			t.downloader.peersC <- announceResponse.Peers
		}
	}
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
