package piece

import (
	"bytes"
	"crypto/sha1"
	"io"
	"os"
	"strconv"
	// "sync"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/partialfile"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
)

type Piece struct {
	index    uint32 // piece index in whole torrent
	hash     []byte
	length   uint32            // last piece may not be complete
	files    partialfile.Files // the place to write downloaded bytes
	blocks   []Block
	bitField bitfield.BitField // blocks we have
	// peers    []*peer           // contains peers that have this piece
	// peersM   sync.Mutex
	// blockC   chan peerBlock
	log logger.Logger
}

type Block struct {
	index  uint32 // block index in piece
	length uint32
	files  partialfile.Files // the place to write downloaded bytes
}

func NewPieces(info *torrent.Info, osFiles []*os.File) []*Piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 = info.GetFiles()[0].Length
		fileEnd    int64 = fileLength // absolute position of end of the file among all pieces
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
	pieces := make([]*Piece, info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		p := &Piece{
			index: i,
			hash:  info.PieceHash(i),
			// blockC: make(chan peerBlock),
			log: logger.New("piece #" + strconv.Itoa(int(i))),
		}

		// Construct p.files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := partialfile.File{osFiles[fileIndex], fileOffset, n}
			p.log.Debugf("file: %#v", file)
			p.files = append(p.files, file)

			left -= n
			p.length += n
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

		p.blocks = newBlocks(p.length, p.files)
		p.bitField = bitfield.New(uint32(len(p.blocks)))
		pieces[i] = p
	}
	return pieces
}

func newBlocks(pieceLength uint32, files partialfile.Files) []Block {
	div, mod := divMod32(pieceLength, protocol.BlockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]Block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = Block{
			index:  j,
			length: protocol.BlockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = Block{
			index:  numBlocks - 1,
			length: uint32(mod),
		}
	}
	var fileIndex int
	var fileOffset uint32
	nextFile := func() {
		fileIndex++
		fileOffset = 0
	}
	fileLeft := func() uint32 { return files[fileIndex].Length - fileOffset }
	for i := range blocks {
		var blockOffset uint32 = 0
		blockLeft := func() uint32 { return blocks[i].length - blockOffset }
		for left := blockLeft(); left > 0 && fileIndex < len(files); {
			n := minUint32(left, fileLeft())
			file := partialfile.File{files[fileIndex].File, files[fileIndex].Offset + int64(fileOffset), n}
			blocks[i].files = append(blocks[i].files, file)
			fileOffset += n
			blockOffset += n
			left -= n
			if fileLeft() == 0 {
				nextFile()
			}
		}
	}
	return blocks
}

func (p *Piece) Index() uint32               { return p.index }
func (p *Piece) Blocks() []Block             { return p.blocks }
func (p *Piece) Length() uint32              { return p.length }
func (p *Piece) Hash() []byte                { return p.hash }
func (p *Piece) BitField() bitfield.BitField { return p.bitField }

func (p *Piece) Reader() io.Reader                 { return p.files.Reader() }
func (p *Piece) Write(b []byte) (n int, err error) { return p.files.Write(b) }

func (p *Piece) HashCheck() (ok bool, err error) {
	hash := sha1.New()
	_, err = io.CopyN(hash, p.files.Reader(), int64(p.Length()))
	if err != nil {
		return
	}
	if bytes.Equal(hash.Sum(nil), p.hash) {
		ok = true
	}
	return
}

func (b *Block) Index() uint32  { return b.index }
func (b *Block) Length() uint32 { return b.length }

func (b *Block) Write(p []byte) (n int, err error) {
	return b.files.Write(p)
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
