package infodownloader

import (
	"bytes"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

const blockSize = 16 * 1024

const maxQueuedBlocks = 10

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	extID     uint8
	totalSize uint32
	Peer      *peer.Peer
	blocks    []block
	limiter   chan struct{}
	DataC     chan Data
	// RejectC chan *piece.Block
	DoneC  chan []byte
	ErrC   chan error
	closeC chan struct{}
}

type Data struct {
	Index uint32
	Data  []byte
}

type block struct {
	index     uint32
	requested bool
	data      []byte
}

func New(pe *peer.Peer, extID uint8, totalSize uint32) *InfoDownloader {
	numBlocks := totalSize / blockSize
	if totalSize > (numBlocks * blockSize) {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for i := range blocks {
		blocks[i] = block{index: uint32(i)}
	}
	return &InfoDownloader{
		extID:     extID,
		totalSize: totalSize,
		Peer:      pe,
		blocks:    blocks,
		limiter:   make(chan struct{}, maxQueuedBlocks),
		DataC:     make(chan Data),
		// RejectC: make(chan *piece.Block),
		DoneC:  make(chan []byte, 1),
		ErrC:   make(chan error, 1),
		closeC: make(chan struct{}),
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
			msg := peerprotocol.ExtensionMessage{
				ExtendedMessageID: d.extID,
				Payload: peerprotocol.ExtensionMetadataMessage{
					Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
					Piece: b.index,
				},
			}
			d.Peer.SendMessage(msg, stopC)
		case msg := <-d.DataC:
			b := &d.blocks[msg.Index]
			if b.requested && b.data == nil && d.limiter != nil {
				<-d.limiter
			}
			b.data = msg.Data
			if d.allDone() {
				d.DoneC <- d.assembleBlocks().Bytes()
				return
			}
		// case blk := <-d.RejectC:
		// 	b := d.blocks[blk.Index]
		// 	if !b.requested {
		// 		d.Peer.Close()
		// 		d.ErrC <- errors.New("received invalid reject message")
		// 		return
		// 	}
		// 	d.blocks[blk.Index].requested = false
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
	buf := bytes.NewBuffer(make([]byte, 0, d.totalSize))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	return buf
}
