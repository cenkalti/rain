package allocator

import (
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/storage"
)

type Allocator struct {
	Files         []File
	NeedHashCheck bool
	Error         error

	closeC chan struct{}
	doneC  chan struct{}
}

type File struct {
	Storage storage.File
	Name    string
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

	defer func() {
		if a.Error != nil {
			for _, f := range a.Files {
				if f.Storage != nil {
					f.Storage.Close()
				}
			}
		}
		select {
		case resultC <- a:
		case <-a.closeC:
		}
	}()

	var allocatedSize int64
	a.Files = make([]File, len(info.Files))
	for i, f := range info.Files {
		var sf storage.File
		var exists bool
		sf, exists, a.Error = sto.Open(f.Path, f.Length)
		if a.Error != nil {
			return
		}
		a.Files[i] = File{Storage: sf, Name: f.Path}
		if exists {
			a.NeedHashCheck = true
		}
		allocatedSize += f.Length
		a.sendProgress(progressC, allocatedSize)
	}
}

func (a *Allocator) sendProgress(progressC chan Progress, size int64) {
	select {
	case progressC <- Progress{AllocatedSize: size}:
	case <-a.closeC:
		return
	}
}
