package piecedownloader

import (
	"time"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

// TODO handle snubbed in piece downloader

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece  *pieceio.Piece
	Peer   *peer.Peer
	Buffer []byte

	requested      map[uint32]struct{}
	nextBlockIndex uint32
	downloadDone   map[uint32]struct{}
	pieceTimeoutC  <-chan time.Time
	queueLength    int
	pieceTimeout   time.Duration
}

type pieceReaderResult struct {
	BlockIndex uint32
	Error      error
}

func New(pi *pieceio.Piece, pe *peer.Peer, queueLength int, pieceTimeout time.Duration) *PieceDownloader {
	return &PieceDownloader{
		Piece: pi,
		Peer:  pe,

		queueLength:  queueLength,
		pieceTimeout: pieceTimeout,
		requested:    make(map[uint32]struct{}),
		Buffer:       make([]byte, pi.Length),
		downloadDone: make(map[uint32]struct{}),
	}
}

func (d *PieceDownloader) Choked() {
	d.pieceTimeoutC = nil
	d.requested = make(map[uint32]struct{})
	d.nextBlockIndex = 0
}

func (d *PieceDownloader) Unchoked() {
	d.RequestBlocks()
}

func (d *PieceDownloader) GotBlock(block *pieceio.Block, data []byte) (allDone bool) {
	if _, ok := d.downloadDone[block.Index]; ok {
		d.Peer.Logger().Warningln("received duplicate block:", block.Index)
	}
	copy(d.Buffer[block.Begin:block.Begin+block.Length], data)
	delete(d.requested, block.Index)
	d.downloadDone[block.Index] = struct{}{}
	d.pieceTimeoutC = nil
	d.RequestBlocks()
	return d.allDone()
}

func (d *PieceDownloader) Rejected(block *pieceio.Block) {
	delete(d.requested, block.Index)
	d.nextBlockIndex = 0
}

func (d *PieceDownloader) CancelPending() {
	for i := range d.requested {
		b := d.Piece.Blocks[i]
		msg := peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}}
		d.Peer.SendMessage(msg)
	}
}

func (d *PieceDownloader) RequestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.Piece.Blocks)) && len(d.requested) < d.queueLength; d.nextBlockIndex++ {
		b := d.Piece.Blocks[d.nextBlockIndex]
		if _, ok := d.downloadDone[b.Index]; ok {
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

func (d *PieceDownloader) allDone() bool {
	return len(d.downloadDone) == len(d.Piece.Blocks)
}
