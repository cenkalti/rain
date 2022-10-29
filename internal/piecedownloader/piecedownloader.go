package piecedownloader

import (
	"errors"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
)

var (
	// ErrBlockDuplicate is returned from PieceDownloader.GotBlock method when the received block is already present.
	ErrBlockDuplicate = errors.New("received duplicate block")
	// ErrBlockNotRequested is returned from PieceDownloader.GotBlock method when the received block is not requested yet.
	ErrBlockNotRequested = errors.New("received not requested block")
	// ErrBlockInvalid is returned from PieceDownloader.GotBlock method when the received block is not in request list.
	ErrBlockInvalid = errors.New("received block is invalid")
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece       *piece.Piece
	Peer        Peer
	AllowedFast bool
	Buffer      bufferpool.Buffer

	// blocks contains blocks that needs to be downloaded from peers.
	// It does not contain the parts that belong to padding files.
	blocks    map[uint32]uint32   // begin -> length
	remaining []uint32            // blocks to be downloaded from peers in consecutive order.
	pending   map[uint32]struct{} // in-flight requests
	done      map[uint32]struct{} // downloaded requests
}

// Peer of a Torrent.
type Peer interface {
	RequestPiece(index, begin, length uint32)
	CancelPiece(index, begin, length uint32)
	EnabledFast() bool
}

// New returns a new PieceDownloader.
func New(pi *piece.Piece, pe Peer, allowedFast bool, buf bufferpool.Buffer) *PieceDownloader {
	blocks := pi.CalculateBlocks()
	return &PieceDownloader{
		Piece:       pi,
		Peer:        pe,
		AllowedFast: allowedFast,
		Buffer:      buf,
		blocks:      makeBlocks(blocks),
		remaining:   makeRemaining(blocks),
		pending:     make(map[uint32]struct{}, len(blocks)),
		done:        make(map[uint32]struct{}, len(blocks)),
	}
}

func makeBlocks(blocks []piece.Block) map[uint32]uint32 {
	ret := make(map[uint32]uint32, len(blocks))
	for _, blk := range blocks {
		ret[blk.Begin] = blk.Length
	}
	return ret
}

func makeRemaining(blocks []piece.Block) []uint32 {
	ret := make([]uint32, len(blocks))
	for i, blk := range blocks {
		ret[i] = blk.Begin
	}
	return ret
}

// Choked must be called when the peer has choked us. This will cancel pending reuqests.
func (d *PieceDownloader) Choked() {
	if d.AllowedFast {
		return
	}
	if d.Peer.EnabledFast() {
		// Peer will send cancel message for pending requests.
		return
	}
	for i := range d.pending {
		delete(d.pending, i)
		d.remaining = append(d.remaining, i)
	}
}

func (d *PieceDownloader) findBlock(begin, length uint32) bool {
	blockLength, ok := d.blocks[begin]
	return ok && blockLength == length
}

// GotBlock must be called when a block is received from the peer.
func (d *PieceDownloader) GotBlock(begin uint32, data []byte) error {
	if !d.findBlock(begin, uint32(len(data))) {
		return ErrBlockInvalid
	}
	if _, ok := d.done[begin]; ok {
		return ErrBlockDuplicate
	}
	copy(d.Buffer.Data[begin:begin+uint32(len(data))], data)
	d.done[begin] = struct{}{}
	if _, ok := d.pending[begin]; !ok {
		// We got the block data although we didn't request it.
		// Data is still saved but error returned here to notify the caller about the issue.
		return ErrBlockNotRequested
	}
	delete(d.pending, begin)
	return nil
}

// Rejected must be called when the peer has rejected a piece request.
func (d *PieceDownloader) Rejected(begin, length uint32) bool {
	if !d.findBlock(begin, length) {
		return false
	}
	delete(d.pending, begin)
	d.remaining = append(d.remaining, begin)
	return true
}

// CancelPending is called to cancel pending requests to the peer.
// Must be called when remaining blocks are downloaded from another peer.
func (d *PieceDownloader) CancelPending() {
	for begin := range d.pending {
		length, ok := d.blocks[begin]
		if !ok {
			panic("cannot get block")
		}
		d.Peer.CancelPiece(d.Piece.Index, begin, length)
	}
}

// RequestBlocks is called to request remaining blocks of the piece up to `queueLength`.
func (d *PieceDownloader) RequestBlocks(queueLength int) {
	remaining := d.remaining
	for _, begin := range remaining {
		if len(d.pending) >= queueLength {
			break
		}
		length, ok := d.blocks[begin]
		if !ok {
			panic("cannot get block")
		}
		if _, ok := d.done[begin]; !ok {
			d.Peer.RequestPiece(d.Piece.Index, begin, length)
		}
		d.remaining = d.remaining[1:]
		d.pending[begin] = struct{}{}
	}
}

// Done returns true if all blocks of the piece has been downloaded.
func (d *PieceDownloader) Done() bool {
	return len(d.done) == len(d.blocks)
}
