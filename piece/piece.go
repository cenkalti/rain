package piece

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
	"os"

	"github.com/cenkalti/rain/filesection"
	"github.com/cenkalti/rain/metainfo"
)

// TODO duplicate
const blockSize = 16 * 1024

// Piece of a torrent.
type Piece struct {
	Index  uint32 // piece index in whole torrent
	OK     bool   // hash is correct and written to disk, Verify() must be called to set this.
	Length uint32 // always equal to blockSize except last piece.
	Blocks []Block
	Files  filesection.Sections // the place to write downloaded bytes
	hash   []byte               // correct hash value
	// Peers         map[[20]byte]struct{} // peers which have this piece, indexed by peer id
	// RequestedFrom map[[20]byte]*Request // peers that we have reqeusted the piece from, indexed by peer id
}

func NewPieces(info *metainfo.Info, osFiles []*os.File) []Piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 = info.GetFiles()[0].Length
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
			// Peers:         make(map[[20]byte]struct{}),
			// RequestedFrom: make(map[[20]byte]*Request),
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
			p.Files = append(p.Files, file)

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
	div, mod := divMod32(p.Length, blockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]Block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = Block{
			Piece:  p,
			Index:  j,
			Begin:  j * blockSize,
			Length: blockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = Block{
			Piece:  p,
			Index:  numBlocks - 1,
			Begin:  (numBlocks - 1) * blockSize,
			Length: mod,
		}
	}
	return blocks
}

// func (p *Piece) CreateRequest(id [20]byte) *Request {
// 	r := &Request{
// 		createdAt:        time.Now(),
// 		BlocksRequesting: bitfield.New(uint32(len(p.Blocks))),
// 		BlocksRequested:  bitfield.New(uint32(len(p.Blocks))),
// 		BlocksReceiving:  bitfield.New(uint32(len(p.Blocks))),
// 		BlocksReceived:   bitfield.New(uint32(len(p.Blocks))),
// 		Data:             make([]byte, p.Length),
// 	}
// 	p.RequestedFrom[id] = r
// 	return r
// }

// func (p *Piece) GetRequest(id [20]byte) *Request {
// 	return p.RequestedFrom[id]
// }

// func (p *Piece) DeleteRequest(id [20]byte) {
// 	delete(p.RequestedFrom, id)
// }

// func (p *Piece) NextBlock(id [20]byte) (*Block, bool) {
// 	i, ok := p.RequestedFrom[id].BlocksRequested.FirstClear(0)
// 	if !ok {
// 		return nil, false
// 	}
// 	return &p.Blocks[i], true
// }

// func (b *Block) deleteRequested(id [20]byte) {
// 	b.Piece.RequestedFrom[id].BlocksRequested.Clear(b.Index)
// }

// func (p *Piece) Availability() int { return len(p.Peers) }

func (p *Piece) Write(b []byte) (n int, err error) {
	hash := sha1.New()
	hash.Write(b)
	if !bytes.Equal(hash.Sum(nil), p.hash) {
		return 0, errors.New("corrupt piece")
	}
	return p.Files.Write(b)
}

// Verify reads from disk and sets p.OK if piece is complete.
func (p *Piece) Verify() error {
	hash := sha1.New()
	if _, err := io.CopyN(hash, p.Files.Reader(), int64(p.Length)); err != nil {
		return err
	}
	p.OK = bytes.Equal(hash.Sum(nil), p.hash)
	return nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
