package allocator

import (
	"path/filepath"

	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
)

type Allocator struct {
	Data          *torrentdata.Data
	NeedHashCheck bool
	Error         error

	info    *metainfo.Info
	storage storage.Storage

	progressC chan Progress
	resultC   chan *Allocator

	closeC chan struct{}
	doneC  chan struct{}
}

type Progress struct {
	AllocatedSize int64
}

func New(info *metainfo.Info, s storage.Storage, progressC chan Progress, resultC chan *Allocator) *Allocator {
	return &Allocator{
		info:      info,
		storage:   s,
		progressC: progressC,
		resultC:   resultC,
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
}

func (a *Allocator) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *Allocator) Run() {
	defer close(a.doneC)

	var files []storage.File
	defer func() {
		if a.Error != nil {
			for _, f := range files {
				if f != nil {
					f.Close()
				}
			}
		}
		a.Data = torrentdata.New(a.info, files)
		select {
		case a.resultC <- a:
		case <-a.closeC:
		}
	}()

	// Single file in torrent
	if !a.info.MultiFile {
		var f storage.File
		f, a.NeedHashCheck, a.Error = a.storage.Open(a.info.Name, a.info.Length)
		if a.Error != nil {
			return
		}
		files = []storage.File{f}
		return
	}

	// Multiple files in torrent grouped in a folder
	files = make([]storage.File, len(a.info.Files))
	for i, f := range a.info.Files {
		parts := append([]string{a.info.Name}, f.Path...)
		path := filepath.Join(parts...)
		var exists bool
		files[i], exists, a.Error = a.storage.Open(path, f.Length)
		if a.Error != nil {
			return
		}
		if exists {
			a.NeedHashCheck = true
		}
	}
}
