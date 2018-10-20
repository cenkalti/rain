package infodownloader

import (
	"bytes"
	"fmt"
	"time"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

const blockSize = 16 * 1024

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	Peer  *peer.Peer
	Bytes []byte
	Error error

	queueLength    int
	pieceTimeout   time.Duration
	extID          uint8
	totalSize      uint32
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	blocks         []block
	pieceTimeoutC  <-chan time.Time
	dataC          chan data
	resultC        chan<- *InfoDownloader
	snubbedC       chan<- *InfoDownloader
	closeC         chan struct{}
	doneC          chan struct{}
}

type data struct {
	Index uint32
	Data  []byte
}

type block struct {
	size uint32
	data []byte
}

func New(pe *peer.Peer, queueLength int, pieceTimeout time.Duration, extID uint8, totalSize uint32, snubbedC chan *InfoDownloader, resultC chan *InfoDownloader) *InfoDownloader {
	numBlocks := totalSize / blockSize
	mod := totalSize % blockSize
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for i := range blocks {
		blocks[i] = block{
			size: blockSize,
		}
	}
	if mod != 0 && len(blocks) > 0 {
		blocks[len(blocks)-1].size = mod
	}
	return &InfoDownloader{
		queueLength:  queueLength,
		pieceTimeout: pieceTimeout,
		extID:        extID,
		totalSize:    totalSize,
		Peer:         pe,
		blocks:       blocks,
		requested:    make(map[uint32]struct{}),
		dataC:        make(chan data),
		snubbedC:     snubbedC,
		resultC:      resultC,
		closeC:       make(chan struct{}),
		doneC:        make(chan struct{}),
	}
}

func (d *InfoDownloader) Close() {
	close(d.closeC)
	<-d.doneC
}

func (d *InfoDownloader) Download(index uint32, b []byte) {
	select {
	case d.dataC <- data{Index: index, Data: b}:
	case <-d.doneC:
	}
}

func (d *InfoDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && len(d.requested) < d.queueLength; d.nextBlockIndex++ {
		msg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: d.extID,
			Payload: peerprotocol.ExtensionMetadataMessage{
				Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
				Piece: d.nextBlockIndex,
			},
		}
		d.Peer.SendMessage(msg)
		d.requested[d.nextBlockIndex] = struct{}{}
	}
	if len(d.requested) > 0 {
		d.pieceTimeoutC = time.After(d.pieceTimeout)
	} else {
		d.pieceTimeoutC = nil
	}
}

func (d *InfoDownloader) Run() {
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
		case msg := <-d.dataC:
			if _, ok := d.requested[msg.Index]; !ok {
				d.Error = fmt.Errorf("peer sent unrequested index for metadata message: %q", msg.Index)
				return
			}
			b := &d.blocks[msg.Index]
			if uint32(len(msg.Data)) != b.size {
				d.Error = fmt.Errorf("peer sent invalid size for metadata message: %q", len(msg.Data))
				return
			}
			delete(d.requested, msg.Index)
			b.data = msg.Data
			if d.allDone() {
				d.Bytes = d.assembleBlocks().Bytes()
				return
			}
			d.pieceTimeoutC = nil
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
