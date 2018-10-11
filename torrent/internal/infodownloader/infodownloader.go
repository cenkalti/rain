package infodownloader

import (
	"bytes"
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

const blockSize = 16 * 1024

const maxQueuedBlocks = 10

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	extID          uint8
	totalSize      uint32
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	Peer           *peer.Peer
	blocks         []block
	DataC          chan Data
	// RejectC chan *piece.Block
	resultC chan Result
	closeC  chan struct{}
}

type Data struct {
	Index uint32
	Data  []byte
}

type block struct {
	index uint32
	size  uint32
	data  []byte
}

type Result struct {
	Peer  *peer.Peer
	Bytes []byte
	Error error
}

func New(pe *peer.Peer, extID uint8, totalSize uint32, resultC chan Result) *InfoDownloader {
	numBlocks := totalSize / blockSize
	mod := totalSize % blockSize
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for i := range blocks {
		blocks[i] = block{
			index: uint32(i),
			size:  blockSize,
		}
	}
	if mod != 0 && len(blocks) > 0 {
		blocks[len(blocks)-1].size = mod
	}
	return &InfoDownloader{
		extID:     extID,
		totalSize: totalSize,
		Peer:      pe,
		blocks:    blocks,
		requested: make(map[uint32]struct{}),
		DataC:     make(chan Data),
		// RejectC: make(chan *piece.Block),
		resultC: resultC,
		closeC:  make(chan struct{}),
	}
}

func (d *InfoDownloader) Close() {
	close(d.closeC)
}

func (d *InfoDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && len(d.requested) < maxQueuedBlocks; d.nextBlockIndex++ {
		d.requested[d.nextBlockIndex] = struct{}{}
		msg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: d.extID,
			Payload: peerprotocol.ExtensionMetadataMessage{
				Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
				Piece: d.nextBlockIndex,
			},
		}
		d.Peer.SendMessage(msg)
	}
}

func (d *InfoDownloader) Run() {
	result := Result{
		Peer: d.Peer,
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
		case msg := <-d.DataC:
			if _, ok := d.requested[msg.Index]; !ok {
				result.Error = fmt.Errorf("peer sent unrequested index for metadata message: %q", msg.Index)
				return
			}
			b := &d.blocks[msg.Index]
			if uint32(len(msg.Data)) != b.size {
				result.Error = fmt.Errorf("peer sent invalid size for metadata message: %q", len(msg.Data))
				return
			}
			delete(d.requested, msg.Index)
			b.data = msg.Data
			if d.allDone() {
				result.Bytes = d.assembleBlocks().Bytes()
				return
			}
			d.requestBlocks()
		// TODO handle rejects in info downloader
		// case blk := <-d.RejectC:
		// 	b := d.blocks[blk.Index]
		// 	if !b.requested {
		// 		d.Peer.Close()
		// 		d.ErrC <- errors.New("received invalid reject message")
		// 		return
		// 	}
		// 	d.blocks[blk.Index].requested = false
		case <-d.closeC:
			return
		}
	}
}

func (d *InfoDownloader) allDone() bool {
	return d.nextBlockIndex == uint32(len(d.blocks)) && len(d.requested) == 0
}

func (d *InfoDownloader) assembleBlocks() *bytes.Buffer {
	buf := bytes.NewBuffer(make([]byte, 0, d.totalSize))
	for i := range d.blocks {
		buf.Write(d.blocks[i].data)
	}
	return buf
}
