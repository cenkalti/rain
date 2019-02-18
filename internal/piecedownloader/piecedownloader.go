package piecedownloader

import (
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/piece"
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece  *piece.Piece
	Peer   *peer.Peer
	Buffer bufferpool.Buffer

	unrequested []uint32
	requested   map[uint32]struct{}
	done        map[uint32]struct{}
}

type pieceReaderResult struct {
	BlockIndex uint32
	Error      error
}

func New(pi *piece.Piece, pe *peer.Peer, buf bufferpool.Buffer) *PieceDownloader {
	unrequested := make([]uint32, len(pi.Blocks))
	for i := range unrequested {
		unrequested[i] = uint32(i)
	}
	return &PieceDownloader{
		Piece:       pi,
		Peer:        pe,
		Buffer:      buf,
		unrequested: unrequested,
		requested:   make(map[uint32]struct{}),
		done:        make(map[uint32]struct{}),
	}
}

func (d *PieceDownloader) Choked() {
	for i := range d.requested {
		d.unrequested = append(d.unrequested, i)
		delete(d.requested, i)
	}
}

func (d *PieceDownloader) GotBlock(block *piece.Block, data []byte) {
	if _, ok := d.done[block.Index]; ok {
		d.Peer.Logger().Warningln("received duplicate block:", block.Index)
	}
	copy(d.Buffer.Data[block.Begin:block.Begin+block.Length], data)
	delete(d.requested, block.Index)
	d.done[block.Index] = struct{}{}
}

func (d *PieceDownloader) Rejected(block *piece.Block) {
	d.unrequested = append(d.unrequested, block.Index)
	delete(d.requested, block.Index)
}

func (d *PieceDownloader) CancelPending() {
	for i := range d.requested {
		b := d.Piece.Blocks[i]
		msg := peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}}
		d.Peer.SendMessage(msg)
	}
}

func (d *PieceDownloader) RequestBlocks(queueLength int) {
	remaining := d.unrequested
	for _, i := range remaining {
		if len(d.requested) >= queueLength {
			break
		}
		b := d.Piece.Blocks[i]
		msg := peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}
		d.Peer.SendMessage(msg)
		d.unrequested = d.unrequested[1:]
		d.requested[b.Index] = struct{}{}
	}
	d.Peer.ResetSnubTimer()
}

func (d *PieceDownloader) Done() bool {
	return len(d.done) == len(d.Piece.Blocks)
}
