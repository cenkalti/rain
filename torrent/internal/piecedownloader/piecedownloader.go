package piecedownloader

import (
	"crypto/sha1" // nolint: gosec
	"errors"
	"time"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece *pieceio.Piece
	Peer  *peer.Peer
	Error error

	queueLength  int
	pieceTimeout time.Duration

	// state
	choked         bool
	buffer         []byte
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	done           map[uint32]struct{}

	// actions received
	pieceC   chan piece
	rejectC  chan *pieceio.Block
	chokeC   chan struct{}
	unchokeC chan struct{}

	// actions sent
	snubbedC chan<- *PieceDownloader
	resultC  chan<- *PieceDownloader

	// internal actions
	pieceReaderResultC chan pieceReaderResult
	pieceWriterResultC chan error
	pieceTimeoutC      <-chan time.Time

	closeC chan struct{}
	doneC  chan struct{}
}

type pieceReaderResult struct {
	BlockIndex uint32
	Error      error
}

func New(pi *pieceio.Piece, pe *peer.Peer, queueLength int, pieceTimeout time.Duration, snubbedC chan *PieceDownloader, resultC chan *PieceDownloader) *PieceDownloader {
	return &PieceDownloader{
		Piece: pi,
		Peer:  pe,

		queueLength:  queueLength,
		pieceTimeout: pieceTimeout,

		buffer:    make([]byte, pi.Length),
		requested: make(map[uint32]struct{}),
		done:      make(map[uint32]struct{}),

		pieceC:   make(chan piece),
		rejectC:  make(chan *pieceio.Block),
		chokeC:   make(chan struct{}),
		unchokeC: make(chan struct{}),

		snubbedC: snubbedC,
		resultC:  resultC,

		pieceReaderResultC: make(chan pieceReaderResult),
		pieceWriterResultC: make(chan error),

		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (d *PieceDownloader) Close() {
	close(d.closeC)
	<-d.doneC
}

func (d *PieceDownloader) Choke() {
	select {
	case d.chokeC <- struct{}{}:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) Unchoke() {
	select {
	case d.unchokeC <- struct{}{}:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) Download(block *pieceio.Block, data []byte) {
	select {
	case d.pieceC <- piece{Block: block, Data: data}:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) Reject(block *pieceio.Block) {
	select {
	case d.rejectC <- block:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) CancelPending() {
	for i := range d.requested {
		b := d.Piece.Blocks[i]
		msg := peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}}
		d.Peer.SendMessage(msg)
	}
}

func (d *PieceDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.Piece.Blocks)) && len(d.requested) < d.queueLength; d.nextBlockIndex++ {
		b := d.Piece.Blocks[d.nextBlockIndex]
		if _, ok := d.done[b.Index]; ok {
			continue
		}
		if _, ok := d.requested[b.Index]; ok {
			continue
		}
		msg := peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}
		d.Peer.SendMessage(msg)
		d.requested[b.Index] = struct{}{}
	}
	if len(d.requested) > 0 {
		d.pieceTimeoutC = time.After(d.pieceTimeout)
	} else {
		d.pieceTimeoutC = nil
	}
}

func (d *PieceDownloader) Run() {
	defer close(d.doneC)

	defer func() {
		select {
		case d.resultC <- d:
		case <-d.closeC:
		}
	}()

	d.requestBlocks()
	for {
		select {
		case p := <-d.pieceC:
			if _, ok := d.done[p.Block.Index]; ok {
				d.Peer.Logger().Warningln("received duplicate block:", p.Block.Index)
			}
			copy(d.buffer[p.Block.Begin:p.Block.Begin+p.Block.Length], p.Data)
			delete(d.requested, p.Block.Index)
			d.done[p.Block.Index] = struct{}{}
			d.pieceTimeoutC = nil
			d.requestBlocks()
			if !d.allDone() {
				break
			}
			ok := d.Piece.VerifyHash(d.buffer, sha1.New()) // nolint: gosec
			if !ok {
				d.Error = errors.New("received corrupt piece")
				return
			}
			go d.pieceWriter()
		case d.Error = <-d.pieceWriterResultC:
			return
		case blk := <-d.rejectC:
			delete(d.requested, blk.Index)
			d.nextBlockIndex = 0
		case <-d.chokeC:
			d.choked = true
			d.pieceTimeoutC = nil
			d.requested = make(map[uint32]struct{})
			d.nextBlockIndex = 0
		case <-d.unchokeC:
			d.choked = false
			d.requestBlocks()
		case <-d.pieceTimeoutC:
			select {
			case d.snubbedC <- d:
			case <-d.closeC:
				return
			}
		case <-d.closeC:
			return
		}
	}
}

func (d *PieceDownloader) allDone() bool {
	return len(d.done) == len(d.Piece.Blocks)
}

func (d *PieceDownloader) pieceWriter() {
	_, err := d.Piece.Data.Write(d.buffer)
	select {
	case d.pieceWriterResultC <- err:
	case <-d.doneC:
	}
}
