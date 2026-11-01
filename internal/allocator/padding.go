package allocator

import "github.com/cenkalti/rain/v2/storage"

type paddingFile struct{}

func newPaddingFile() storage.File {
	return paddingFile{}
}

var _ storage.File = paddingFile{}

func (f paddingFile) ReadAt(p []byte, off int64) (n int, err error) {
	// Need to serve zeroes to be backwards compatible with clients that do not support padding files.
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func (f paddingFile) WriteAt(p []byte, off int64) (n int, err error) {
	panic("attempt to write padding file")
}

func (f paddingFile) Close() error {
	return nil
}
