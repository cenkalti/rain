package piecedownloader

import (
	"errors"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
)

var (
	ErrBlockDuplicate    = errors.New("received duplicate block")
	ErrBlockNotRequested = errors.New("received not requested block")
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece       *piece.Piece
	Peer        Peer
	AllowedFast bool
	Buffer      bufferpool.Buffer

	unrequested []int
	requested   map[int]struct{} // in-flight requests
	done        map[int]struct{} // downloaded requests
}

type Peer interface {
	RequestPiece(index, begin, length uint32)
	CancelPiece(index, begin, length uint32)
}

func New(pi *piece.Piece, pe Peer, allowedFast bool, buf bufferpool.Buffer) *PieceDownloader {
	unrequested := make([]int, pi.NumBlocks())
	for i := range unrequested {
		unrequested[i] = i
	}
	return &PieceDownloader{
		Piece:       pi,
		Peer:        pe,
		AllowedFast: allowedFast,
		Buffer:      buf,
		unrequested: unrequested,
		requested:   make(map[int]struct{}),
		done:        make(map[int]struct{}),
	}
}

func (d *PieceDownloader) Choked() {
	for i := range d.requested {
		d.unrequested = append(d.unrequested, i)
		delete(d.requested, i)
	}
}

func (d *PieceDownloader) GotBlock(block piece.Block, data []byte) error {
	var err error
	if _, ok := d.done[block.Index]; ok {
		return ErrBlockDuplicate
	} else if _, ok := d.requested[block.Index]; !ok {
		err = ErrBlockNotRequested
	}
	copy(d.Buffer.Data[block.Begin:block.Begin+block.Length], data)
	delete(d.requested, block.Index)
	d.done[block.Index] = struct{}{}
	return err
}

func (d *PieceDownloader) Rejected(block piece.Block) {
	d.unrequested = append(d.unrequested, block.Index)
	delete(d.requested, block.Index)
}

func (d *PieceDownloader) CancelPending() {
	for i := range d.requested {
		b, ok := d.Piece.GetBlock(i)
		if !ok {
			panic("cannot get block")
		}
		d.Peer.CancelPiece(d.Piece.Index, b.Begin, b.Length)
	}
}

func (d *PieceDownloader) RequestBlocks(queueLength int) {
	remaining := d.unrequested
	for _, i := range remaining {
		if len(d.requested) >= queueLength {
			break
		}
		b, ok := d.Piece.GetBlock(i)
		if !ok {
			panic("cannot get block")
		}
		d.Peer.RequestPiece(d.Piece.Index, b.Begin, b.Length)
		d.unrequested = d.unrequested[1:]
		d.requested[b.Index] = struct{}{}
	}
}

func (d *PieceDownloader) Done() bool {
	return len(d.done) == d.Piece.NumBlocks()
}
