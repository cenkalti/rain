package partialfile

import (
	"io"
	"os"
)

type File struct {
	File   *os.File
	Offset int64
	Length uint32
}

type Files []File

func (f Files) Reader() io.Reader { return &reader{files: f} }
func (f Files) Writer() io.Writer { return &writer{files: f} }

type reader struct {
	files []File // remaining files
	pos   int64  // position in current file
}

func (r *reader) Read(b []byte) (n int, err error) {
	if len(r.files) == 0 {
		err = io.EOF
		return
	}
	f := r.files[0]
	n, err = f.File.ReadAt(b, f.Offset+r.pos)
	r.pos += int64(n)
	if err == io.EOF {
		r.files = r.files[1:] // advance to next file
		r.pos = 0             // reset position
		if len(r.files) > 0 {
			err = nil
		}
	}
	return
}

type writer struct {
	files []File
}

func (w *writer) Write(b []byte) (n int, err error) {
	var m int
	for _, f := range w.files {
		m, err = f.File.WriteAt(b[:f.Length], f.Offset)
		n += m
		if err != nil {
			return
		}
		if uint32(m) != f.Length {
			err = io.ErrShortWrite
			return
		}
		b = b[m:]
	}
	return
}
