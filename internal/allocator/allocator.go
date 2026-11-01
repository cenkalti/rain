package allocator

import (
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/storage"
)

// Allocator allocates files on the disk.
type Allocator struct {
	Files       []File
	HasExisting bool
	HasMissing  bool
	Error       error

	closeC chan struct{}
	doneC  chan struct{}
}

// File on the disk.
type File struct {
	Storage storage.File
	Name    string
	Padding bool
}

// Progress about the allocation.
type Progress struct {
	AllocatedSize int64
}

// New returns a new Allocator.
func New() *Allocator {
	return &Allocator{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the Allocator.
func (a *Allocator) Close() {
	close(a.closeC)
	<-a.doneC
}

// Run the Allocator.
func (a *Allocator) Run(info *metainfo.Info, sto storage.Storage, progressC chan Progress, resultC chan *Allocator) {
	defer close(a.doneC)

	defer func() {
		if a.Error != nil {
			a.closeFiles()
		}
		select {
		case resultC <- a:
		case <-a.closeC:
			// Result is not delivered, nobody else will close the files.
			if a.Error == nil {
				a.closeFiles()
			}
		}
	}()

	var allocatedSize int64
	a.Files = make([]File, len(info.Files))
	for i, f := range info.Files {
		var sf storage.File
		var exists bool
		if f.Padding {
			sf = newPaddingFile()
		} else {
			sf, exists, a.Error = sto.Open(f.Path, f.Length)
			if a.Error != nil {
				return
			}
			if exists {
				a.HasExisting = true
			} else {
				a.HasMissing = true
			}
		}
		a.Files[i] = File{Storage: sf, Name: f.Path, Padding: f.Padding}
		allocatedSize += f.Length
		if !a.sendProgress(progressC, allocatedSize) {
			return
		}
	}
}

func (a *Allocator) sendProgress(progressC chan Progress, size int64) bool {
	select {
	case progressC <- Progress{AllocatedSize: size}:
		return true
	case <-a.closeC:
		return false
	}
}

func (a *Allocator) closeFiles() {
	for _, f := range a.Files {
		if f.Storage != nil {
			f.Storage.Close()
		}
	}
}
