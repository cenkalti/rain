package infodownloader

import "fmt"

const blockSize = 16 * 1024

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	Peer  Peer
	Bytes []byte

	blocks         []block
	numRequested   int // in-flight requests
	nextBlockIndex uint32
}

type block struct {
	size      uint32
	requested bool
}

type Peer interface {
	MetadataSize() uint32
	RequestMetadataPiece(index uint32)
}

func New(pe Peer) *InfoDownloader {
	d := &InfoDownloader{
		Peer:  pe,
		Bytes: make([]byte, pe.MetadataSize()),
	}
	d.blocks = d.createBlocks()
	return d
}

func (d *InfoDownloader) GotBlock(index uint32, data []byte) error {
	if index >= uint32(len(d.blocks)) {
		return fmt.Errorf("peer sent invalid metadata piece index: %q", index)
	}
	b := &d.blocks[index]
	if !b.requested {
		return fmt.Errorf("peer sent unrequested index for metadata message: %q", index)
	}
	if uint32(len(data)) != b.size {
		return fmt.Errorf("peer sent invalid size for metadata message: %q", len(data))
	}
	d.numRequested--
	begin := index * blockSize
	end := begin + b.size
	copy(d.Bytes[begin:end], data)
	return nil
}

func (d *InfoDownloader) createBlocks() []block {
	numBlocks := d.Peer.MetadataSize() / blockSize
	mod := d.Peer.MetadataSize() % blockSize
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

func (d *InfoDownloader) RequestBlocks(queueLength int) {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && d.numRequested < queueLength; d.nextBlockIndex++ {
		d.Peer.RequestMetadataPiece(d.nextBlockIndex)
		d.blocks[d.nextBlockIndex].requested = true
		d.numRequested++
	}
}

func (d *InfoDownloader) Done() bool {
	return d.nextBlockIndex == uint32(len(d.blocks)) && d.numRequested == 0
}
