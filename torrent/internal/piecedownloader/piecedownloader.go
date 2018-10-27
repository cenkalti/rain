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

	requested map[uint32]struct{}

	pieceC   chan piece
	rejectC  chan *pieceio.Block
	chokeC   chan struct{}
	unchokeC chan struct{}
	closeC   chan struct{}
	doneC    chan struct{}
}

type pieceReaderResult struct {
	BlockIndex uint32
	Error      error
}

func New(pi *pieceio.Piece, pe *peer.Peer) *PieceDownloader {
	return &PieceDownloader{
		Piece: pi,
		Peer:  pe,

		requested: make(map[uint32]struct{}),
		pieceC:    make(chan piece),
		rejectC:   make(chan *pieceio.Block),
		chokeC:    make(chan struct{}),
		unchokeC:  make(chan struct{}),
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
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

func (d *PieceDownloader) Run(queueLength int, pieceTimeout time.Duration, snubbedC chan *PieceDownloader, resultC chan *PieceDownloader) {
	defer close(d.doneC)

	defer func() {
		select {
		case resultC <- d:
		case <-d.closeC:
		}
	}()

	var (
		nextBlockIndex     uint32
		buffer             = make([]byte, d.Piece.Length)
		done               = make(map[uint32]struct{})
		pieceWriterResultC = make(chan error)
		pieceTimeoutC      <-chan time.Time
		sendSnubbedC       chan *PieceDownloader
	)

	requestBlocks := func() {
		for ; nextBlockIndex < uint32(len(d.Piece.Blocks)) && len(d.requested) < queueLength; nextBlockIndex++ {
			b := d.Piece.Blocks[nextBlockIndex]
			if _, ok := done[b.Index]; ok {
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
			pieceTimeoutC = time.After(pieceTimeout)
		} else {
			pieceTimeoutC = nil
		}
	}

	allDone := func() bool {
		return len(done) == len(d.Piece.Blocks)
	}

	pieceWriter := func() {
		_, err := d.Piece.Data.Write(buffer)
		select {
		case pieceWriterResultC <- err:
		case <-d.doneC:
		}
	}

	requestBlocks()
	for {
		select {
		case p := <-d.pieceC:
			if _, ok := done[p.Block.Index]; ok {
				d.Peer.Logger().Warningln("received duplicate block:", p.Block.Index)
			}
			copy(buffer[p.Block.Begin:p.Block.Begin+p.Block.Length], p.Data)
			delete(d.requested, p.Block.Index)
			done[p.Block.Index] = struct{}{}
			pieceTimeoutC = nil
			requestBlocks()
			if !allDone() {
				break
			}
			ok := d.Piece.VerifyHash(buffer, sha1.New()) // nolint: gosec
			if !ok {
				d.Error = errors.New("received corrupt piece")
				return
			}
			go pieceWriter()
		case d.Error = <-pieceWriterResultC:
			return
		case blk := <-d.rejectC:
			delete(d.requested, blk.Index)
			nextBlockIndex = 0
		case <-d.chokeC:
			pieceTimeoutC = nil
			d.requested = make(map[uint32]struct{})
			nextBlockIndex = 0
		case <-d.unchokeC:
			requestBlocks()
		case <-pieceTimeoutC:
			sendSnubbedC = snubbedC
		case sendSnubbedC <- d:
			sendSnubbedC = nil
		case <-d.closeC:
			return
		}
	}
}
