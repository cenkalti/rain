package rain

import "io"

type section struct {
	File   readwriterAt
	Offset int64
	Length int64
}

type readwriterAt interface {
	io.ReaderAt
	io.WriterAt
}

type sections []section

func (s sections) Reader() io.Reader {
	readers := make([]io.Reader, len(s))
	for i := range s {
		readers[i] = io.NewSectionReader(s[i].File, s[i].Offset, int64(s[i].Length))
	}
	return io.MultiReader(readers...)
}

// ReadAt is used when uploading blocks of a piece.
func (s sections) ReadAt(p []byte, off int64) (n int, err error) {
	var readers []io.Reader
	var i int
	var sec section
	var pos int64
	// Skip sections up to offset
	for i, sec = range s {
		pos += sec.Length
		if pos > off {
			break
		}
	}
	// Add half section
	if sec.File != nil {
		advance := sec.Length - (pos - off)
		sr := io.NewSectionReader(sec.File, sec.Offset+advance, int64(sec.Length-advance))
		readers = append(readers, sr)
	}
	// Add remaining sections
	for i++; i < len(s); i++ {
		readers[i] = io.NewSectionReader(s[i].File, s[i].Offset, int64(s[i].Length))
	}
	return io.ReadFull(io.MultiReader(readers...), p)
}

// Write is used when writing a downloaded piece (all blocks) after hash check is done.
func (s sections) Write(p []byte) (n int, err error) {
	var m int
	for _, sec := range s {
		m, err = sec.File.WriteAt(p[:sec.Length], sec.Offset)
		n += m
		if err != nil {
			return
		}
		if int64(m) < sec.Length {
			err = io.ErrShortWrite
			return
		}
		p = p[m:]
	}
	return
}
