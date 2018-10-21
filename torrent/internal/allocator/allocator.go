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

	closeC chan struct{}
	doneC  chan struct{}
}

type Progress struct {
	AllocatedSize int64
}

func New() *Allocator {
	return &Allocator{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (a *Allocator) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *Allocator) Run(info *metainfo.Info, sto storage.Storage, progressC chan Progress, resultC chan *Allocator) {
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
		a.Data = torrentdata.New(info, files)
		select {
		case resultC <- a:
		case <-a.closeC:
		}
	}()

	// Single file in torrent
	if !info.MultiFile {
		var f storage.File
		f, a.NeedHashCheck, a.Error = sto.Open(info.Name, info.Length)
		if a.Error != nil {
			return
		}
		files = []storage.File{f}
		return
	}

	// Multiple files in torrent grouped in a folder
	files = make([]storage.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{info.Name}, f.Path...)
		path := filepath.Join(parts...)
		var exists bool
		files[i], exists, a.Error = sto.Open(path, f.Length)
		if a.Error != nil {
			return
		}
		if exists {
			a.NeedHashCheck = true
		}
	}
}
