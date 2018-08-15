package piecedownloader

import (
	"bytes"
	"errors"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

const maxQueuedBlocks = 10

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	piece   *piece.Piece
	peer    *peer.Peer
	blocks  []block
	limiter chan struct{}
	PieceC  chan peer.Piece
	Done    chan struct{}
	Err     error
}

type block struct {
	*piece.Block
	requested bool
	data      []byte
}

func New(pi *piece.Piece, pe *peer.Peer) *PieceDownloader {
	blocks := make([]block, len(pi.Blocks))
	for i := range blocks {
		blocks[i] = block{Block: &pi.Blocks[i]}
	}
	return &PieceDownloader{
		piece:   pi,
		peer:    pe,
		blocks:  blocks,
		limiter: make(chan struct{}, maxQueuedBlocks),
		PieceC:  make(chan peer.Piece),
		Done:    make(chan struct{}),
	}
}

func (d *PieceDownloader) Run(stopC chan struct{}) {
	defer close(d.Done)
	for {
		select {
		case d.limiter <- struct{}{}:
			b := d.nextBlock()
			err := d.peer.SendRequest(d.piece.Index, b.Begin, b.Length)
			if err != nil {
				d.Err = err
				return
			}
		case p := <-d.PieceC:
			b := d.blocks[p.Block.Index]
			if b.requested && b.data == nil {
				<-d.limiter
			}
			b.data = p.Data
			if d.allDone() {
				d.Err = d.verifyPiece()
				return
			}
			// TODO handle choke
			// TODO handle unchoke
		case <-d.peer.NotifyDisconnect():
			d.Err = errors.New("peer disconnected")
			return
		case <-stopC:
			d.Err = errors.New("download stopped")
			return
		}
	}
}

func (d *PieceDownloader) nextBlock() *block {
	for i := range d.blocks {
		if !d.blocks[i].requested {
			return &d.blocks[i]
		}
	}
	return nil
}

func (d *PieceDownloader) allDone() bool {
	for i := range d.blocks {
		if d.blocks[i].data == nil {
			return false
		}
	}
	return true
}

func (d *PieceDownloader) numRequested() int {
	var n int
	for i := range d.blocks {
		if d.blocks[i].requested {
			n++
		}
	}
	return n
}

func (d *PieceDownloader) verifyPiece() error {
	buf := bytes.NewBuffer(make([]byte, 0, d.piece.Length))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	_, err := d.piece.Write(buf.Bytes())
	return err
}
