package partialfile

import (
	"errors"
	"os"
)

var ErrInvalidLength = errors.New("invalid slice length")

type File struct {
	File   *os.File
	Offset int64
	Length uint32
}

func (f File) Read(b []byte) (n int, err error) {
	if len(b) != int(f.Length) {
		return 0, ErrInvalidLength
	}
	return f.File.ReadAt(b[:f.Length], f.Offset)
}

func (f File) Write(b []byte) (n int, err error) {
	if len(b) != int(f.Length) {
		return 0, ErrInvalidLength
	}
	return f.File.WriteAt(b[:f.Length], f.Offset)
}

type Files []File

func (f Files) Read(b []byte) (n int, err error) {
	var total uint32
	for _, p := range f {
		total += p.Length
	}
	if len(b) != int(total) {
		return 0, ErrInvalidLength
	}
	for _, p := range f {
		m, e := p.Read(b[:p.Length])
		if e != nil {
			err = e
			break
		}
		n += m
		b = b[m:]
	}
	return
}

func (f Files) Write(b []byte) (n int, err error) {
	var total uint32
	for _, p := range f {
		total += p.Length
	}
	if len(b) != int(total) {
		return 0, ErrInvalidLength
	}
	for _, p := range f {
		m, e := p.Write(b[:p.Length])
		if e != nil {
			err = e
			break
		}
		n += m
		b = b[m:]
	}
	return
}
