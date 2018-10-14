package piecedownloader

import (
	"crypto/sha1" // nolint: gosec
	"errors"
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

const (
	maxQueuedBlocks = 10
	pieceTimeout    = 20 * time.Second
	requestTimeout  = 60 * time.Second
)

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece *pieceio.Piece
	Peer  *peer.Peer

	// state
	choked         bool
	buffer         []byte
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	downloading    map[uint32]struct{}
	done           map[uint32]struct{}

	// actions received
	pieceC   chan piece
	rejectC  chan *pieceio.Block
	chokeC   chan struct{}
	unchokeC chan struct{}

	// actions sent
	snubbedC chan<- *PieceDownloader
	resultC  chan<- Result

	// internal actions
	pieceReaderResultC chan pieceReaderResult
	pieceWriterResultC chan error
	pieceTimeoutC      <-chan time.Time

	closeC chan struct{}
	doneC  chan struct{}
}

type Result struct {
	Peer  *peer.Peer
	Piece *pieceio.Piece
	Error error
}

type pieceReaderResult struct {
	BlockIndex uint32
	Error      error
}

func New(pi *pieceio.Piece, pe *peer.Peer, snubbedC chan *PieceDownloader, resultC chan Result) *PieceDownloader {
	return &PieceDownloader{
		Piece: pi,
		Peer:  pe,

		buffer:      make([]byte, pi.Length),
		requested:   make(map[uint32]struct{}),
		downloading: make(map[uint32]struct{}),
		done:        make(map[uint32]struct{}),

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

func (d *PieceDownloader) Download(block *pieceio.Block, conn net.Conn, doneC chan struct{}) {
	select {
	case d.pieceC <- piece{Block: block, Conn: conn, DoneC: doneC}:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) Reject(block *pieceio.Block) {
	select {
	case d.rejectC <- block:
	case <-d.doneC:
	}
}

func (d *PieceDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.Piece.Blocks)) && len(d.requested) < maxQueuedBlocks; d.nextBlockIndex++ {
		b := d.Piece.Blocks[d.nextBlockIndex]
		if _, ok := d.done[b.Index]; ok {
			continue
		}
		if _, ok := d.downloading[b.Index]; ok {
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
		d.pieceTimeoutC = time.After(pieceTimeout)
	} else {
		d.pieceTimeoutC = nil
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
		case p := <-d.pieceC:
			if _, ok := d.done[p.Block.Index]; ok {
				panic("got piece twice")
			}
			delete(d.requested, p.Block.Index)
			d.downloading[p.Block.Index] = struct{}{}
			d.pieceTimeoutC = nil
			go d.pieceReader(p)
		case res := <-d.pieceReaderResultC:
			delete(d.downloading, res.BlockIndex)
			d.done[res.BlockIndex] = struct{}{}
			d.requestBlocks()
			if d.allDone() {
				ok := d.Piece.VerifyHash(d.buffer, sha1.New()) // nolint: gosec
				if !ok {
					result.Error = errors.New("received corrupt piece")
					return
				}
				go d.pieceWriter()
			}
		case result.Error = <-d.pieceWriterResultC:
			return
		case blk := <-d.rejectC:
			delete(d.requested, blk.Index)
			d.nextBlockIndex = 0
		case <-d.chokeC:
			d.choked = true
			d.pieceTimeoutC = nil
			d.requested = make(map[uint32]struct{})
			d.downloading = make(map[uint32]struct{})
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

func (d *PieceDownloader) pieceReader(p piece) {
	result := pieceReaderResult{
		BlockIndex: p.Block.Index,
	}
	defer func() {
		if result.Error == nil {
			close(p.DoneC)
		}
		select {
		case d.pieceReaderResultC <- result:
		case <-d.closeC:
		}
	}()
	result.Error = p.Conn.SetReadDeadline(time.Now().Add(requestTimeout))
	if result.Error != nil {
		return
	}
	var n int
	b := d.buffer[p.Block.Begin : p.Block.Begin+p.Block.Length]
	n, result.Error = io.ReadFull(p.Conn, b)
	if nerr, ok := result.Error.(net.Error); ok && nerr.Timeout() {
		// Peer couldn't send the block in allowed time.
		select {
		case d.snubbedC <- d:
		case <-d.closeC:
			return
		}
		result.Error = p.Conn.SetReadDeadline(time.Now().Add(peerreader.ReadTimeout - requestTimeout))
		if result.Error != nil {
			return
		}
		_, result.Error = io.ReadFull(p.Conn, b[n:])
	}
}

func (d *PieceDownloader) pieceWriter() {
	_, err := d.Piece.Data.Write(d.buffer)
	select {
	case d.pieceWriterResultC <- err:
	case <-d.doneC:
	}
}
