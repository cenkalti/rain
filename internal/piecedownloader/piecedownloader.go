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
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece       *piece.Piece
	Peer        Peer
	AllowedFast bool
	Buffer      bufferpool.Buffer

	remaining []int
	pending   map[int]struct{} // in-flight requests
	done      map[int]struct{} // downloaded requests
}

// Peer of a Torrent.
type Peer interface {
	RequestPiece(index, begin, length uint32)
	CancelPiece(index, begin, length uint32)
	EnabledFast() bool
}

// New returns a new PieceDownloader.
func New(pi *piece.Piece, pe Peer, allowedFast bool, buf bufferpool.Buffer) *PieceDownloader {
	remaining := make([]int, pi.NumBlocks())
	for i := range remaining {
		remaining[i] = i
	}
	return &PieceDownloader{
		Piece:       pi,
		Peer:        pe,
		AllowedFast: allowedFast,
		Buffer:      buf,
		remaining:   remaining,
		pending:     make(map[int]struct{}),
		done:        make(map[int]struct{}),
	}
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

// GotBlock must be called when a block is received from the piece.
func (d *PieceDownloader) GotBlock(block piece.Block, data []byte) error {
	var err error
	if _, ok := d.done[block.Index]; ok {
		return ErrBlockDuplicate
	} else if _, ok := d.pending[block.Index]; !ok {
		err = ErrBlockNotRequested
	}
	copy(d.Buffer.Data[block.Begin:block.Begin+block.Length], data)
	delete(d.pending, block.Index)
	d.done[block.Index] = struct{}{}
	return err
}

// Rejected must be called when the peer has rejected a piece request.
func (d *PieceDownloader) Rejected(block piece.Block) {
	delete(d.pending, block.Index)
	d.remaining = append(d.remaining, block.Index)
}

// CancelPending is called to cancel pending requests to the peer.
// Must be called when remaining blocks are downloaded from another peer.
func (d *PieceDownloader) CancelPending() {
	for i := range d.pending {
		b, ok := d.Piece.GetBlock(i)
		if !ok {
			panic("cannot get block")
		}
		d.Peer.CancelPiece(d.Piece.Index, b.Begin, b.Length)
	}
}

// RequestBlocks is called to request remaining blocks of the piece up to `queueLength`.
func (d *PieceDownloader) RequestBlocks(queueLength int) {
	remaining := d.remaining
	for _, i := range remaining {
		if len(d.pending) >= queueLength {
			break
		}
		b, ok := d.Piece.GetBlock(i)
		if !ok {
			panic("cannot get block")
		}
		if _, ok := d.done[i]; !ok {
			d.Peer.RequestPiece(d.Piece.Index, b.Begin, b.Length)
		}
		d.remaining = d.remaining[1:]
		d.pending[i] = struct{}{}
	}
}

// Done returns true if all blocks of the piece has been downloaded.
func (d *PieceDownloader) Done() bool {
	return len(d.done) == d.Piece.NumBlocks()
}
