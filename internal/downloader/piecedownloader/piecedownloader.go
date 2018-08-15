package piecedownloader

import (
	"bytes"
	"errors"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

const maxQueuedBlocks = 10

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece    *piece.Piece
	Peer     *peer.Peer
	blocks   []block
	limiter  chan struct{}
	PieceC   chan peer.Piece
	ChokeC   chan struct{}
	UnchokeC chan struct{}
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
		Piece:    pi,
		Peer:     pe,
		blocks:   blocks,
		limiter:  make(chan struct{}, maxQueuedBlocks),
		PieceC:   make(chan peer.Piece),
		ChokeC:   make(chan struct{}),
		UnchokeC: make(chan struct{}),
	}
}

func (d *PieceDownloader) Run(stopC chan struct{}) error {
	for {
		select {
		case d.limiter <- struct{}{}:
			b := d.nextBlock()
			if b == nil {
				d.limiter = nil
				break
			}
			err := d.Peer.SendRequest(d.Piece.Index, b.Begin, b.Length)
			if err != nil {
				return err
			}
		case p := <-d.PieceC:
			log.Warningln("piece message", p.Piece.Index, p.Block.Index, p.Block.Begin, p.Block.Length)
			b := &d.blocks[p.Block.Index]
			if b.requested && b.data == nil && d.limiter != nil {
				<-d.limiter
			}
			b.data = p.Data
			if d.allDone() {
				return d.verifyPiece()
			}
		case <-d.ChokeC:
			for i := range d.blocks {
				if d.blocks[i].data == nil && d.blocks[i].requested {
					d.blocks[i].requested = false
				}
			}
			// d.limiter = nil
		case <-d.UnchokeC:
			// d.limiter = make(chan struct{}, maxQueuedBlocks)
		case <-d.Peer.NotifyDisconnect():
			return errors.New("peer disconnected")
		case <-stopC:
			return errors.New("download stopped")
		}
	}
}

func (d *PieceDownloader) nextBlock() *block {
	for i := range d.blocks {
		if !d.blocks[i].requested {
			d.blocks[i].requested = true
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
	buf := bytes.NewBuffer(make([]byte, 0, d.Piece.Length))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	err := d.Piece.Write(buf.Bytes())
	return err
}
