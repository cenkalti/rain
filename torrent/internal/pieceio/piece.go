package pieceio

import (
	"bytes"
	"hash"
	"io"

	"github.com/cenkalti/rain/torrent/internal/filesection"
	"github.com/cenkalti/rain/torrent/metainfo"
	"github.com/cenkalti/rain/torrent/storage"
)

// Piece of a torrent.
type Piece struct {
	Index  uint32 // index in torrent
	Length uint32 // always equal to Info.PieceLength except last piece
	Blocks Blocks
	Data   Data // the place to write downloaded bytes
	Hash   []byte
}

type Data interface {
	io.ReaderAt
	io.Writer
}

func NewPieces(info *metainfo.Info, osFiles []storage.File) []Piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 // length of the file in fileIndex
		fileEnd    int64 // absolute position of end of the file among all pieces
		fileOffset int64 // offset in file: [0, fileLength)
	)

	nextFile := func() {
		fileIndex++
		fileLength = info.GetFiles()[fileIndex].Length
		fileEnd += fileLength
		fileOffset = 0
	}

	// Init first file
	fileIndex = -1
	nextFile()

	fileLeft := func() int64 { return fileLength - fileOffset }

	// Construct pieces
	var total int64
	pieces := make([]Piece, info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		p := Piece{
			Index: i,
			Hash:  info.PieceHashes[i],
		}

		var sections filesection.Sections

		// Construct p.Files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := filesection.Section{
				File:   osFiles[fileIndex],
				Offset: fileOffset,
				Length: int64(n),
			}
			sections = append(sections, file)

			left -= n
			p.Length += n
			pieceOffset += n
			fileOffset += int64(n)
			total += int64(n)

			if total == info.TotalLength {
				break
			}
			if fileLeft() == 0 {
				nextFile()
			}
		}

		p.Data = sections
		p.Blocks = newBlocks(p.Length)
		pieces[i] = p
	}
	return pieces
}

func (p *Piece) VerifyHash(buf []byte, h hash.Hash) bool {
	if uint32(len(buf)) != p.Length {
		return false
	}
	h.Write(buf)
	sum := h.Sum(nil)
	return bytes.Equal(sum, p.Hash)
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
