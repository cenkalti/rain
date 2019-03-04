package infodownloader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestPeer struct {
	requested []uint32
}

func (p *TestPeer) MetadataSize() uint32 { return 10*16*1024 + 42 }
func (p *TestPeer) RequestMetadataPiece(index uint32) {
	p.requested = append(p.requested, index)
}

func TestInfoDownloader(t *testing.T) {
	p := &TestPeer{}
	d := New(p)
	assert.Equal(t, 11, len(d.blocks))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 4, d.pending)
	assert.False(t, d.Done())
	assert.Equal(t, []uint32{0, 1, 2, 3}, p.requested)

	d.RequestBlocks(4)
	assert.Equal(t, 4, d.pending)
	assert.Equal(t, []uint32{0, 1, 2, 3}, p.requested)

	assert.Nil(t, d.GotBlock(0, make([]byte, blockSize)))
	assert.Equal(t, 3, d.pending)
	d.RequestBlocks(4)
	assert.Equal(t, 4, d.pending)
	assert.Equal(t, []uint32{0, 1, 2, 3, 4}, p.requested)

	d.GotBlock(1, make([]byte, blockSize))
	d.GotBlock(2, make([]byte, blockSize))
	d.GotBlock(3, make([]byte, blockSize))
	d.GotBlock(4, make([]byte, blockSize))
	assert.Equal(t, 0, d.pending)
	d.RequestBlocks(4)
	assert.Equal(t, 4, d.pending)
	assert.Equal(t, []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8}, p.requested)

	println("---")
	d.GotBlock(5, make([]byte, blockSize))
	d.GotBlock(6, make([]byte, blockSize))
	d.GotBlock(7, make([]byte, blockSize))
	d.GotBlock(8, make([]byte, blockSize))
	assert.Equal(t, 0, d.pending)
	d.RequestBlocks(4)
	assert.Equal(t, 2, d.pending)
	assert.False(t, d.Done())

	d.GotBlock(9, make([]byte, blockSize))
	d.GotBlock(10, make([]byte, 42))
	assert.True(t, d.Done())
}
