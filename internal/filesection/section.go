package filesection

import "io"

// FileSection of a file.
type FileSection struct {
	File   ReadWriterAt
	Offset int64
	Length int64
	Name   string
	// Padding indicates that the file is used as padding so it should not be requested from peers.
	// The contents of padding files are always zero.
	Padding bool
}

// ReadWriterAt combines the io.ReaderAt and io.WriterAt interfaces.
type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

// Piece is contiguous sections of files. When piece hashes in torrent file is being calculated
// all files are concatenated and splitted into pieces in length specified in the torrent file.
type Piece []FileSection

// ReadAt implements io.ReaderAt interface.
// It reads bytes from s at given offset into p.
// Used when uploading blocks of a piece.
func (p Piece) ReadAt(b []byte, off int64) (int, error) {
	var readers []io.Reader
	var i int
	var pos int64

	// Skip sections up to offset
	for ; i < len(p); i++ {
		pos += p[i].Length
		if pos >= off {
			break
		}
	}

	// Add half section
	advance := p[i].Length - (pos - off)
	readers = append(readers, io.NewSectionReader(p[i].File, p[i].Offset+advance, p[i].Length-advance))

	// Add remaining sections
	for i++; i < len(p); i++ {
		readers = append(readers, io.NewSectionReader(p[i].File, p[i].Offset, p[i].Length))
		pos += p[i].Length
		if pos >= off+int64(len(b)) {
			break
		}
	}

	return io.ReadFull(io.MultiReader(readers...), b)
}

// Write implements io.Writer interface.
// It writes the bytes in p into files in s.
// Used when writing a downloaded piece (all blocks) after hash check is done.
// Calling write does not change the current position in s,
// so len(p) must be equal to total length of the all files in s in order to issue a full write.
func (p Piece) Write(b []byte) (n int, err error) {
	var m int
	for _, sec := range p {
		if sec.Padding {
			continue
		}
		m, err = sec.File.WriteAt(b[:sec.Length], sec.Offset)
		n += m
		if err != nil {
			return
		}
		b = b[m:]
	}
	return
}
