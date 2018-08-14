package downloader

import (
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type pieceDownloader struct {
	piece *Piece
	peer  *Peer
	// data  []byte
	pieceC chan peer.Piece
}

func newPieceDownloader(pi *Piece, pe *Peer) *pieceDownloader {
	return &pieceDownloader{
		piece:  pi,
		peer:   pe,
		pieceC: make(chan peer.Piece),
	}
}

func (d *pieceDownloader) run(stopC chan struct{}) {
	blocksRequested := make(chan struct{}, maxQueuedBlocks)
	blocksRemaining := d.piece.Blocks
	for {
		if len(blocksRemaining) == 0 {
			// TODO handle finish
			return
		}
		select {
		case blocksRequested <- struct{}{}:
			var b piece.Block
			b, blocksRemaining = blocksRemaining[0], blocksRemaining[1:]
			err := d.peer.SendRequest(uint32(d.piece.index), b.Begin, b.Length)
			if err != nil {
				// TODO handle error
			}
		// case p := <-d.pieceC:
		// p.
		case <-stopC:
			return
		}
	}
}
