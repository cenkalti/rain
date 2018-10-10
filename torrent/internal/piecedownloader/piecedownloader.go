package piecedownloader

import (
	"bytes"

	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
	"github.com/cenkalti/rain/torrent/internal/semaphore"
)

const maxQueuedBlocks = 10

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece     *pieceio.Piece
	Peer      *peerconn.Conn
	blocks    []block
	semaphore *semaphore.Semaphore
	PieceC    chan Piece
	RejectC   chan *pieceio.Block
	ChokeC    chan struct{}
	UnchokeC  chan struct{}
	resultC   chan Result
	closeC    chan struct{}
	doneC     chan struct{}
}

type block struct {
	*pieceio.Block
	requested bool
	data      []byte
}

type Result struct {
	Peer  *peerconn.Conn
	Piece *pieceio.Piece
	Bytes []byte
}

func New(pi *pieceio.Piece, pe *peerconn.Conn, resultC chan Result) *PieceDownloader {
	blocks := make([]block, len(pi.Blocks))
	for i := range blocks {
		blocks[i] = block{Block: &pi.Blocks[i]}
	}
	return &PieceDownloader{
		Piece:     pi,
		Peer:      pe,
		blocks:    blocks,
		semaphore: semaphore.New(maxQueuedBlocks),
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

	d.semaphore.Start()
	for {
		select {
		case <-d.semaphore.Wait:
			b := d.nextBlock()
			if b == nil {
				d.semaphore.Stop()
				break
			}
			msg := peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}
			d.Peer.SendMessage(msg)
		case p := <-d.PieceC:
			b := &d.blocks[p.Block.Index]
			if b.requested && b.data == nil {
				d.semaphore.Signal(1)
			}
			b.data = p.Data
			if d.allDone() {
				result.Bytes = d.assembleBlocks().Bytes()
				return
			}
		case blk := <-d.RejectC:
			b := d.blocks[blk.Index]
			if !b.requested {
				d.Peer.Logger().Warningln("received reject message for block not requested yet")
				break
			}
			d.blocks[blk.Index].requested = false
		case <-d.ChokeC:
			for i := range d.blocks {
				if d.blocks[i].data == nil && d.blocks[i].requested {
					d.blocks[i].requested = false
				}
			}
			d.semaphore.Stop()
		case <-d.UnchokeC:
			d.semaphore.Start()
		case <-d.closeC:
			return
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

func (d *PieceDownloader) assembleBlocks() *bytes.Buffer {
	buf := bytes.NewBuffer(make([]byte, 0, d.Piece.Length))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	return buf
}
