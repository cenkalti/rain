package infodownloader

import "fmt"

const blockSize = 16 * 1024

// InfoDownloader downloads all blocks of a piece from a peer.
type InfoDownloader struct {
	Peer  Peer
	Bytes []byte

	blocks         []block
	pending        int // in-flight requests
	nextBlockIndex uint32
}

type block struct {
	size      uint32
	requested bool
}

// Peer of a torrent.
type Peer interface {
	MetadataSize() uint32
	RequestMetadataPiece(index uint32)
}

// New return new InfoDownloader for a single Peer.
func New(pe Peer) *InfoDownloader {
	d := &InfoDownloader{
		Peer:  pe,
		Bytes: make([]byte, pe.MetadataSize()),
	}
	d.blocks = d.createBlocks()
	return d
}

// GotBlock must be called when a metadata block is received from the peer.
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
	d.pending--
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

// RequestBlocks is called to request remaining blocks of metadata from the peer.
func (d *InfoDownloader) RequestBlocks(queueLength int) {
	for ; d.nextBlockIndex < uint32(len(d.blocks)) && d.pending < queueLength; d.nextBlockIndex++ {
		d.Peer.RequestMetadataPiece(d.nextBlockIndex)
		d.blocks[d.nextBlockIndex].requested = true
		d.pending++
	}
}

// Done returns true if all pieces of the metadata is downloaded.
func (d *InfoDownloader) Done() bool {
	return d.nextBlockIndex == uint32(len(d.blocks)) && d.pending == 0
}
