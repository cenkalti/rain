package partialfile

import "io"

type File struct {
	File   readwriterAt
	Offset int64
	Length int64
}

type Files []File

type readwriterAt interface {
	io.ReaderAt
	io.WriterAt
}

func (f Files) Reader() io.Reader {
	readers := make([]io.Reader, len(f))
	for i := range f {
		readers[i] = io.NewSectionReader(f[i].File, f[i].Offset, int64(f[i].Length))
	}
	return io.MultiReader(readers...)
}

func (f Files) Write(b []byte) (n int, err error) {
	var m int
	for _, file := range f {
		m, err = file.File.WriteAt(b[:file.Length], file.Offset)
		n += m
		if err != nil {
			return
		}
		if int64(m) < file.Length {
			err = io.ErrShortWrite
			return
		}
		b = b[m:]
	}
	return
}
