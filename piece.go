package rain

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
	"os"
	"time"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/filesection"
	"github.com/cenkalti/rain/torrent"
)

type piece struct {
	Index         uint32 // piece index in whole torrent
	OK            bool   // hash is correct and written to disk, Verify() must be called to set this.
	Length        uint32 // last piece may not be complete
	Blocks        []block
	hash          []byte                      // correct hash value
	files         filesection.Sections        // the place to write downloaded bytes
	peers         map[[20]byte]struct{}       // peers which have this piece, indexed by peer id
	requestedFrom map[[20]byte]*activeRequest // peers that we have reqeusted the piece from, indexed by peer id
}

type block struct {
	Piece  *piece
	Index  uint32 // index in piece
	Begin  uint32 // offset in piece
	Length uint32
}

type activeRequest struct {
	createdAt        time.Time
	blocksRequesting *bitfield.Bitfield
	blocksRequested  *bitfield.Bitfield
	blocksReceiving  *bitfield.Bitfield
	blocksReceived   *bitfield.Bitfield
	data             []byte // buffer for received blocks
}

func (p *piece) getActiveRequest(id [20]byte) *activeRequest { return p.requestedFrom[id] }
func (p *piece) deleteActiveRequest(id [20]byte)             { delete(p.requestedFrom, id) }
func (p *piece) createActiveRequest(id [20]byte) *activeRequest {
	r := &activeRequest{
		createdAt:        time.Now(),
		blocksRequesting: bitfield.New(uint32(len(p.Blocks))),
		blocksRequested:  bitfield.New(uint32(len(p.Blocks))),
		blocksReceiving:  bitfield.New(uint32(len(p.Blocks))),
		blocksReceived:   bitfield.New(uint32(len(p.Blocks))),
		data:             make([]byte, p.Length),
	}
	p.requestedFrom[id] = r
	return r
}
func (r *activeRequest) resetWaitingRequests() {
	r.blocksRequesting.ClearAll()
	r.blocksRequested.ClearAll()
	copy(r.blocksRequesting.Bytes(), r.blocksReceiving.Bytes())
	copy(r.blocksRequested.Bytes(), r.blocksReceiving.Bytes())
}
func (r *activeRequest) outstanding() uint32 {
	o := int64(r.blocksRequested.Count()) - int64(r.blocksReceiving.Count())
	if o < 0 {
		o = 0
	}
	return uint32(o)
}

func (p *piece) nextBlock(id [20]byte) (*block, bool) {
	i, ok := p.requestedFrom[id].blocksRequested.FirstClear(0)
	if !ok {
		return nil, false
	}
	return &p.Blocks[i], true
}

// func (b *block) deleteRequested(id [20]byte) {
// 	b.Piece.requestedFrom[id].blocksRequested.Clear(b.Index)
// }

func newPieces(info *torrent.Info, osFiles []*os.File) []*piece {
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
	pieces := make([]*piece, info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		p := &piece{
			Index:         i,
			hash:          info.PieceHash(i),
			peers:         make(map[[20]byte]struct{}),
			requestedFrom: make(map[[20]byte]*activeRequest),
		}

		// Construct p.files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := filesection.Section{
				File:   osFiles[fileIndex],
				Offset: fileOffset,
				Length: int64(n),
			}
			p.files = append(p.files, file)

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

func (p *piece) newBlocks() []block {
	div, mod := divMod32(p.Length, blockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = block{
			Piece:  p,
			Index:  j,
			Begin:  j * blockSize,
			Length: blockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = block{
			Piece:  p,
			Index:  numBlocks - 1,
			Begin:  (numBlocks - 1) * blockSize,
			Length: mod,
		}
	}
	return blocks
}

func (p *piece) availability() int { return len(p.peers) }

func (p *piece) Write(b []byte) (n int, err error) {
	hash := sha1.New()
	hash.Write(b)
	if !bytes.Equal(hash.Sum(nil), p.hash) {
		return 0, errors.New("corrupt piece")
	}
	return p.files.Write(b)
}

// Verify reads from disk and sets p.OK if piece is complete.
func (p *piece) Verify() error {
	hash := sha1.New()
	if _, err := io.CopyN(hash, p.files.Reader(), int64(p.Length)); err != nil {
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
