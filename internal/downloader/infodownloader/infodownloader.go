package infodownloader

import (
	"bytes"
	"errors"

	"github.com/cenkalti/rain/internal/peer"
	// "github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/piece"
)

const maxQueuedBlocks = 10

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	Piece    *piece.Piece
	Peer     *peer.Peer
	blocks   []block
	limiter  chan struct{}
	PieceC   chan *piece.Piece
	RejectC  chan *piece.Block
	ChokeC   chan struct{}
	UnchokeC chan struct{}
	DoneC    chan []byte
	ErrC     chan error
	closeC   chan struct{}
}

type block struct {
	*piece.Block
	requested bool
	data      []byte
}

func New(pi *piece.Piece, pe *peer.Peer) *InfoDownloader {
	blocks := make([]block, len(pi.Blocks))
	for i := range blocks {
		blocks[i] = block{Block: &pi.Blocks[i]}
	}
	return &InfoDownloader{
		Piece:    pi,
		Peer:     pe,
		blocks:   blocks,
		limiter:  make(chan struct{}, maxQueuedBlocks),
		PieceC:   make(chan *piece.Piece),
		RejectC:  make(chan *piece.Block),
		ChokeC:   make(chan struct{}),
		UnchokeC: make(chan struct{}),
		DoneC:    make(chan []byte, 1),
		ErrC:     make(chan error, 1),
		closeC:   make(chan struct{}),
	}
}

func (d *InfoDownloader) Close() {
	close(d.closeC)
}

func (d *InfoDownloader) Run(stopC chan struct{}) {
	for {
		select {
		case d.limiter <- struct{}{}:
			b := d.nextBlock()
			if b == nil {
				d.limiter = nil
				break
			}
			// msg := peerprotocol.ExtensionMessage{Request{}}
			// d.Peer.SendMessage(msg, stopC)
		// case _ = <-d.DataC:
		// b := &d.blocks[0]
		// if b.requested && b.data == nil && d.limiter != nil {
		// 	<-d.limiter
		// }
		// // b.data = p.Data
		// if d.allDone() {
		// 	d.DoneC <- d.assembleBlocks().Bytes()
		// 	return
		// }
		case blk := <-d.RejectC:
			b := d.blocks[blk.Index]
			if !b.requested {
				d.Peer.Close()
				d.ErrC <- errors.New("received invalid reject message")
				return
			}
			d.blocks[blk.Index].requested = false
		case <-stopC:
			return
		case <-d.closeC:
			return
		}
	}
}

func (d *InfoDownloader) nextBlock() *block {
	for i := range d.blocks {
		if !d.blocks[i].requested {
			d.blocks[i].requested = true
			return &d.blocks[i]
		}
	}
	return nil
}

func (d *InfoDownloader) allDone() bool {
	for i := range d.blocks {
		if d.blocks[i].data == nil {
			return false
		}
	}
	return true
}

func (d *InfoDownloader) assembleBlocks() *bytes.Buffer {
	buf := bytes.NewBuffer(make([]byte, 0, d.Piece.Length))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	return buf
}
