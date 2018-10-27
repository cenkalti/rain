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

	dataC  chan data
	closeC chan struct{}
	doneC  chan struct{}
}

type data struct {
	Index uint32
	Data  []byte
}

type block struct {
	size uint32
	data []byte
}

func New(pe *peer.Peer) *InfoDownloader {
	return &InfoDownloader{
		Peer:   pe,
		dataC:  make(chan data),
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
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

func (d *InfoDownloader) Run(queueLength int, pieceTimeout time.Duration, snubbedC chan *InfoDownloader, resultC chan *InfoDownloader) {
	defer close(d.doneC)
	defer func() {
		select {
		case resultC <- d:
		case <-d.closeC:
		}
	}()

	var (
		blocks         = d.createBlocks()
		requested      = make(map[uint32]struct{})
		nextBlockIndex uint32
		pieceTimeoutC  <-chan time.Time
		sendSnubbedC   chan *InfoDownloader
	)

	requestBlocks := func() {
		for ; nextBlockIndex < uint32(len(blocks)) && len(requested) < queueLength; nextBlockIndex++ {
			msg := peerprotocol.ExtensionMessage{
				ExtendedMessageID: d.Peer.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey],
				Payload: peerprotocol.ExtensionMetadataMessage{
					Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
					Piece: nextBlockIndex,
				},
			}
			d.Peer.SendMessage(msg)
			requested[nextBlockIndex] = struct{}{}
		}
		if len(requested) > 0 {
			pieceTimeoutC = time.After(pieceTimeout)
		} else {
			pieceTimeoutC = nil
		}
	}

	allDone := func() bool {
		return nextBlockIndex == uint32(len(blocks)) && len(requested) == 0
	}

	assembleBlocks := func() *bytes.Buffer {
		buf := bytes.NewBuffer(make([]byte, 0, d.Peer.ExtensionHandshake.MetadataSize))
		for i := range blocks {
			buf.Write(blocks[i].data)
		}
		return buf
	}

	requestBlocks()
	for {
		select {
		case msg := <-d.dataC:
			if _, ok := requested[msg.Index]; !ok {
				d.Error = fmt.Errorf("peer sent unrequested index for metadata message: %q", msg.Index)
				return
			}
			b := &blocks[msg.Index]
			if uint32(len(msg.Data)) != b.size {
				d.Error = fmt.Errorf("peer sent invalid size for metadata message: %q", len(msg.Data))
				return
			}
			delete(requested, msg.Index)
			b.data = msg.Data
			if allDone() {
				d.Bytes = assembleBlocks().Bytes()
				return
			}
			pieceTimeoutC = nil
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

func (d *InfoDownloader) createBlocks() []block {
	numBlocks := d.Peer.ExtensionHandshake.MetadataSize / blockSize
	mod := d.Peer.ExtensionHandshake.MetadataSize % blockSize
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
	return blocks
}
