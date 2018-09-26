package piece

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"hash"

	"github.com/cenkalti/rain/internal/filesection"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/storage"
)

const BlockSize = 16 * 1024

// Piece of a torrent.
type Piece struct {
	Index  uint32 // index in torrent
	Length uint32 // always equal to BlockSize except last piece.
	Blocks []Block
	Data   filesection.Sections // the place to write downloaded bytes
	Hash   []byte
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
			p.Data = append(p.Data, file)

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

		p.Blocks = p.newBlocks()
		pieces[i] = p
	}
	return pieces
}

func (p *Piece) newBlocks() []Block {
	div, mod := divMod32(p.Length, BlockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]Block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = Block{
			Index:  j,
			Begin:  j * BlockSize,
			Length: BlockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = Block{
			Index:  numBlocks - 1,
			Begin:  (numBlocks - 1) * BlockSize,
			Length: mod,
		}
	}
	return blocks
}

func (p *Piece) FindBlock(begin, length uint32) *Block {
	idx, mod := divMod32(begin, BlockSize)
	if mod != 0 {
		return nil
	}
	if idx >= uint32(len(p.Blocks)) {
		return nil
	}
	b := &p.Blocks[idx]
	if b.Length != length {
		return nil
	}
	return b
}

func (p *Piece) Verify(buf []byte) bool {
	return p.VerifyHash(buf, sha1.New()) // nolint: gosec
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
