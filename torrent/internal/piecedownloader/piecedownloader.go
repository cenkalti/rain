package piecedownloader

import (
	"bytes"
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

const maxQueuedBlocks = 10

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece          *pieceio.Piece
	Peer           *peer.Peer
	blocks         []block
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	PieceC         chan Piece
	RejectC        chan *pieceio.Block
	ChokeC         chan struct{}
	UnchokeC       chan struct{}
	resultC        chan Result
	closeC         chan struct{}
	doneC          chan struct{}
}

type block struct {
	*pieceio.Block
	data []byte
}

type Result struct {
	Peer  *peer.Peer
	Piece *pieceio.Piece
	Bytes []byte
	Error error
}

func New(pi *pieceio.Piece, pe *peer.Peer, resultC chan Result) *PieceDownloader {
	blocks := make([]block, len(pi.Blocks))
	for i := range blocks {
		blocks[i] = block{Block: &pi.Blocks[i]}
	}
	return &PieceDownloader{
		Piece:     pi,
		Peer:      pe,
		blocks:    blocks,
		requested: make(map[uint32]struct{}),
		PieceC:    make(chan Piece),
		RejectC:   make(chan *pieceio.Block),
		ChokeC:    make(chan struct{}),
		UnchokeC:  make(chan struct{}),
		resultC:   resultC,
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
}

func (d *PieceDownloader) Close() {
	close(d.closeC)
}

func (d *PieceDownloader) Done() <-chan struct{} {
	return d.doneC
}

func (d *PieceDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && len(d.requested) < maxQueuedBlocks; d.nextBlockIndex++ {
		b := d.blocks[d.nextBlockIndex]
		if b.data != nil {
			continue
		}
		d.requested[d.nextBlockIndex] = struct{}{}
		msg := peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}
		d.Peer.SendMessage(msg)
	}
}

func (d *PieceDownloader) Run() {
	defer close(d.doneC)

	result := Result{
		Peer:  d.Peer,
		Piece: d.Piece,
	}
	defer func() {
		select {
		case d.resultC <- result:
		case <-d.closeC:
		}
	}()

	d.requestBlocks()
	for {
		select {
		case p := <-d.PieceC:
			if _, ok := d.requested[p.Block.Index]; !ok {
				result.Error = fmt.Errorf("peer sent unrequested piece block: %q", p.Block)
				return
			}
			b := &d.blocks[p.Block.Index]
			delete(d.requested, p.Block.Index)
			b.data = p.Data
			if d.allDone() {
				result.Bytes = d.assembleBlocks().Bytes()
				return
			}
			d.requestBlocks()
		case blk := <-d.RejectC:
			if _, ok := d.requested[blk.Index]; !ok {
				result.Error = fmt.Errorf("peer sent reject to unrequested block: %q", blk)
				return
			}
			delete(d.requested, blk.Index)
			d.nextBlockIndex = 0
		case <-d.ChokeC:
			d.requested = make(map[uint32]struct{})
			d.nextBlockIndex = 0
		case <-d.UnchokeC:
			d.requestBlocks()
		case <-d.closeC:
			return
		}
	}
}

func (d *PieceDownloader) allDone() bool {
	for i := range d.blocks {
		if d.blocks[i].data == nil {
			return false
		}
	}
	return true
}

func (d *PieceDownloader) assembleBlocks() *bytes.Buffer {
	buf := bytes.NewBuffer(make([]byte, 0, d.Piece.Length))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	return buf
}
