package storage

type PaddingFile struct{}

func NewPaddingFile(length int64) File {
	return PaddingFile{}
}

var _ File = PaddingFile{}

func (f PaddingFile) ReadAt(p []byte, off int64) (n int, err error) {
	// Need to serve zeroes to be backwards compatible with clients that do not support padding files.
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func (f PaddingFile) WriteAt(p []byte, off int64) (n int, err error) {
	panic("attempt to write padding file")
}

func (f PaddingFile) Close() error {
	return nil
}
