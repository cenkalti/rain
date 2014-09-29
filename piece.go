package rain

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
	"os"
	"time"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/bt"
	"github.com/cenkalti/rain/torrent"
)

type Piece struct {
	Index         uint32 // piece index in whole torrent
	OK            bool   // hash is correct and written to disk, Verify() must be called to set this.
	Length        uint32 // last piece may not be complete
	Blocks        []Block
	hash          []byte                       // correct hash value
	files         sections                     // the place to write downloaded bytes
	peers         map[bt.PeerID]struct{}       // peers which have this piece
	requestedFrom map[bt.PeerID]*activeRequest // peers that we have reqeusted the piece from
}

type Block struct {
	Piece  *Piece
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

func (p *Piece) getActiveRequest(id bt.PeerID) *activeRequest { return p.requestedFrom[id] }
func (p *Piece) deleteActiveRequest(id bt.PeerID)             { delete(p.requestedFrom, id) }
func (p *Piece) createActiveRequest(id bt.PeerID) *activeRequest {
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

func (p *Piece) nextBlock(id bt.PeerID) (*Block, bool) {
	i, ok := p.requestedFrom[id].blocksRequested.FirstClear(0)
	if !ok {
		return nil, false
	}
	return &p.Blocks[i], true
}

func (b *Block) deleteRequested(id bt.PeerID) {
	b.Piece.requestedFrom[id].blocksRequested.Clear(b.Index)
}

func newPieces(info *torrent.Info, osFiles []*os.File) []*Piece {
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
			Index:         i,
			hash:          info.PieceHash(i),
			peers:         make(map[bt.PeerID]struct{}),
			requestedFrom: make(map[bt.PeerID]*activeRequest),
		}

		// Construct p.files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := section{osFiles[fileIndex], fileOffset, int64(n)}
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
			Length: uint32(mod),
		}
	}
	return blocks
}

func (p *Piece) availability() int { return len(p.peers) }
func (p *Piece) Reader() io.Reader { return p.files.Reader() }

func (p *Piece) Write(b []byte) (n int, err error) {
	hash := sha1.New()
	hash.Write(b)
	if !bytes.Equal(hash.Sum(nil), p.hash) {
		return 0, errors.New("corrupt piece")
	}
	return p.files.Write(b)
}

// Verify reads from disk and sets p.OK if piece is complete.
func (p *Piece) Verify() error {
	hash := sha1.New()
	if _, err := io.CopyN(hash, p.files.Reader(), int64(p.Length)); err != nil {
		return err
	}
	p.OK = bytes.Equal(hash.Sum(nil), p.hash)
	return nil
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
