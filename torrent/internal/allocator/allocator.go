package allocator

import (
	"path/filepath"

	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/torrentdata"
)

type Allocator struct {
	info    *metainfo.Info
	storage storage.Storage

	progressC chan Progress
	resultC   chan Result

	closeC chan struct{}
	doneC  chan struct{}
}

type Progress struct {
	AllocatedSize int64
}

type Result struct {
	Data          *torrentdata.Data
	NeedHashCheck bool
	Error         error
}

func New(info *metainfo.Info, s storage.Storage, progressC chan Progress, resultC chan Result) *Allocator {
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

	var result Result
	var files []storage.File
	defer func() {
		if result.Error != nil {
			for _, f := range files {
				if f != nil {
					f.Close()
				}
			}
		}
		result.Data = torrentdata.New(a.info, files)
		select {
		case a.resultC <- result:
		case <-a.closeC:
		}
	}()

	// Single file in torrent
	if !a.info.MultiFile {
		var f storage.File
		f, result.NeedHashCheck, result.Error = a.storage.Open(a.info.Name, a.info.Length)
		if result.Error != nil {
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
		files[i], exists, result.Error = a.storage.Open(path, f.Length)
		if result.Error != nil {
			return
		}
		if exists {
			result.NeedHashCheck = true
		}
	}
}
