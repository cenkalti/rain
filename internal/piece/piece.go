package piece

import (
	"bytes"
	"hash"

	"github.com/cenkalti/rain/internal/allocator"
	"github.com/cenkalti/rain/internal/filesection"
	"github.com/cenkalti/rain/internal/metainfo"
)

// BlockSize is the size of smallest piece data that we are going to request from peers.
const BlockSize = 16 * 1024

// Piece of a torrent.
type Piece struct {
	Index   uint32            // index in torrent
	Length  uint32            // always equal to Info.PieceLength except last piece
	Data    filesection.Piece // the place to write downloaded bytes
	Hash    []byte
	Writing bool
	Done    bool
}

// Block is part of a Piece that is specified in peerprotocol.Request messages.
type Block struct {
	Index  int    // index in piece
	Begin  uint32 // offset in piece
	Length uint32 // always equal to BlockSize except the last block of a piece.
}

// NewPieces returns a slice of Pieces by mapping files to the pieces.
func NewPieces(info *metainfo.Info, files []allocator.File) []Piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 // length of the file in fileIndex
		fileEnd    int64 // absolute position of end of the file among all pieces
		fileOffset int64 // offset in file: [0, fileLength)
	)

	nextFile := func() {
		fileIndex++
		fileLength = info.Files[fileIndex].Length
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
			Hash:  info.PieceHash(i),
		}

		var sections filesection.Piece

		// Construct p.Files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := filesection.FileSection{
				File:   files[fileIndex].Storage,
				Offset: fileOffset,
				Length: int64(n),
				Name:   files[fileIndex].Name,
			}
			sections = append(sections, file)

			left -= n
			p.Length += n
			pieceOffset += n
			fileOffset += int64(n)
			total += int64(n)

			if total == info.Length {
				break
			}
			if fileLeft() == 0 {
				nextFile()
			}
		}

		p.Data = sections
		pieces[i] = p
	}
	return pieces
}

// NumBlocks returns the number of blocks in the piece.
func (p *Piece) NumBlocks() int {
	div, mod := divMod32(p.Length, BlockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	return int(numBlocks)
}

// GetBlock returns the Block at index i.
func (p *Piece) GetBlock(i int) (b Block, ok bool) {
	div, mod := divMod32(p.Length, BlockSize)
	numBlocks := int(div)
	if mod != 0 {
		numBlocks++
	}
	if i >= numBlocks {
		return
	}
	var blen uint32
	if mod != 0 && i == numBlocks-1 {
		blen = mod
	} else {
		blen = BlockSize
	}
	return Block{
		Index:  i,
		Begin:  uint32(i) * BlockSize,
		Length: blen,
	}, true
}

// FindBlock returns the block at offset `begin` and length `length`.
func (p *Piece) FindBlock(begin, length uint32) (b Block, ok bool) {
	idx, mod := divMod32(begin, BlockSize)
	if mod != 0 {
		return
	}
	b, ok = p.GetBlock(int(idx))
	if !ok {
		return
	}
	if b.Length != length {
		ok = false
		return
	}
	return b, true
}

// VerifyHash returns true if hash of piece data in buffer `buf` matches the hash of Piece.
func (p *Piece) VerifyHash(buf []byte, h hash.Hash) bool {
	if uint32(len(buf)) != p.Length {
		return false
	}
	_, _ = h.Write(buf)
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
