package infodownloader

import (
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

	blocks         []block
	requested      map[uint32]struct{}
	nextBlockIndex uint32
	queueLength    int
	pieceTimeout   time.Duration
	pieceTimeoutC  <-chan time.Time
}

type block struct {
	size uint32
}

func New(pe *peer.Peer, queueLength int, pieceTimeout time.Duration) *InfoDownloader {
	d := &InfoDownloader{
		Peer:         pe,
		Bytes:        make([]byte, pe.ExtensionHandshake.MetadataSize),
		queueLength:  queueLength,
		pieceTimeout: pieceTimeout,
		requested:    make(map[uint32]struct{}),
	}
	d.blocks = d.createBlocks()
	return d
}

func (d *InfoDownloader) GotBlock(index uint32, data []byte) error {
	if _, ok := d.requested[index]; !ok {
		return fmt.Errorf("peer sent unrequested index for metadata message: %q", index)
	}
	b := &d.blocks[index]
	if uint32(len(data)) != b.size {
		return fmt.Errorf("peer sent invalid size for metadata message: %q", len(data))
	}
	delete(d.requested, index)
	begin := index * blockSize
	end := begin + b.size
	copy(d.Bytes[begin:end], data)
	return nil
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

func (d *InfoDownloader) RequestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && len(d.requested) < d.queueLength; d.nextBlockIndex++ {
		msg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: d.Peer.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata],
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

func (d *InfoDownloader) Done() bool {
	return d.nextBlockIndex == uint32(len(d.blocks)) && len(d.requested) == 0
}
