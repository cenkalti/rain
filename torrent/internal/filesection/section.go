package filesection

import "io"

// FileSection of a file.
type FileSection struct {
	File   ReadWriterAt
	Offset int64
	Length int64
}

type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

// Piece is contiguous sections of files. When piece hashes in torrent file is being calculated
// all files are concatenated and splitted into pieces in length specified in the torrent file.
type Piece []FileSection

func (p Piece) ReadFull(buf []byte) error {
	readers := make([]io.Reader, len(p))
	for i := range p {
		readers[i] = io.NewSectionReader(p[i].File, p[i].Offset, p[i].Length)
	}
	r := io.MultiReader(readers...)
	_, err := io.ReadFull(r, buf)
	return err
}

// ReadAt implements io.ReaderAt interface.
// It reads bytes from s at given offset into p.
// Used when uploading blocks of a piece.
func (s Piece) ReadAt(p []byte, off int64) (int, error) {
	var readers []io.Reader
	var i int
	var pos int64

	// Skip sections up to offset
	for ; i < len(s); i++ {
		pos += s[i].Length
		if pos >= off {
			break
		}
	}

	// Add half section
	advance := s[i].Length - (pos - off)
	readers = append(readers, io.NewSectionReader(s[i].File, s[i].Offset+advance, s[i].Length-advance))

	// Add remaining sections
	for i++; i < len(s); i++ {
		readers = append(readers, io.NewSectionReader(s[i].File, s[i].Offset, s[i].Length))
		pos += s[i].Length
		if pos >= off+int64(len(p)) {
			break
		}
	}

	return io.ReadFull(io.MultiReader(readers...), p)
}

// Write implements io.Writer interface.
// It writes the bytes in p into files in s.
// Used when writing a downloaded piece (all blocks) after hash check is done.
// Calling write does not change the current position in s,
// so len(p) must be equal to total length of the all files in s in order to issue a full write.
func (p Piece) Write(b []byte) (n int, err error) {
	var m int
	for _, sec := range p {
		m, err = sec.File.WriteAt(b[:sec.Length], sec.Offset)
		n += m
		if err != nil {
			return
		}
		if int64(m) < sec.Length {
			err = io.ErrShortWrite
			return
		}
		b = b[m:]
	}
	return
}
