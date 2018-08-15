package piece

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
	"os"

	"github.com/cenkalti/rain/internal/filesection"
	"github.com/cenkalti/rain/internal/metainfo"
)

const BlockSize = 16 * 1024

// Piece of a torrent.
type Piece struct {
	Index  uint32 // index in torrent
	Length uint32 // always equal to BlockSize except last piece.
	Blocks []Block
	// TODO do not export sections, export a data interface
	Data filesection.Sections // the place to write downloaded bytes
	OK   bool                 // hash is correct and written to disk, Verify() must be called to set this.
	hash []byte               // correct hash value
}

func NewPieces(info *metainfo.Info, osFiles []*os.File) []Piece {
	// TODO take interface rwcloser for osFiles
	// TODO init all to zero, call nextFile to init
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 = info.GetFiles()[fileIndex].Length
		fileEnd          = fileLength // absolute position of end of the file among all pieces
		fileOffset int64              // offset in file: [0, fileLength)
	)

	nextFile := func() {
		fileIndex++
		fileLength = info.GetFiles()[fileIndex].Length
		fileEnd += fileLength
		fileOffset = 0
	}
	fileLeft := func() int64 { return fileLength - fileOffset }

	// Construct pieces
	var total int64
	pieces := make([]Piece, info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		p := Piece{
			Index: i,
			hash:  info.PieceHash(i),
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

func (p *Piece) Write(b []byte) error {
	if uint32(len(b)) != p.Length {
		return errors.New("invalid piece data length")
	}
	hash := sha1.New()
	hash.Write(b)
	if !bytes.Equal(hash.Sum(nil), p.hash) {
		return errors.New("corrupt piece")
	}
	_, err := p.Data.Write(b)
	if err != nil {
		return err
	}
	p.OK = true
	return nil
}

// Verify reads from disk and sets p.OK if piece is complete.
func (p *Piece) Verify() error {
	hash := sha1.New()
	if _, err := io.CopyN(hash, p.Data.Reader(), int64(p.Length)); err != nil {
		return err
	}
	p.OK = bytes.Equal(hash.Sum(nil), p.hash)
	return nil
}

func (p *Piece) GetBlock(begin uint32) *Block {
	idx, mod := divMod32(begin, BlockSize)
	if mod != 0 {
		return nil
	}
	if idx >= uint32(len(p.Blocks)) {
		return nil
	}
	return &p.Blocks[idx]
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
