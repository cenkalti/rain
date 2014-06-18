package rain

import (
	"errors"
	"os"
)

var errInvalidLength = errors.New("invalid slice length")

type partialFile struct {
	file   *os.File
	offset int64
	length int32
}

func (f partialFile) Read(b []byte) (n int, err error) {
	if len(b) != int(f.length) {
		return 0, errInvalidLength
	}
	return f.file.ReadAt(b[:f.length], f.offset)
}

func (f partialFile) Write(b []byte) (n int, err error) {
	if len(b) != int(f.length) {
		return 0, errInvalidLength
	}
	return f.file.WriteAt(b[:f.length], f.offset)
}

type partialFiles []partialFile

func (f partialFiles) Read(b []byte) (n int, err error) {
	var total int32
	for _, p := range f {
		total += p.length
	}
	if len(b) != int(total) {
		return 0, errInvalidLength
	}
	for _, p := range f {
		m, e := p.Read(b[:p.length])
		if e != nil {
			err = e
			break
		}
		n += m
		b = b[:m]
	}
	return
}
