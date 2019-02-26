package piece

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNumBlocks(t *testing.T) {
	p := Piece{Length: 2 * 16 * 1024}
	assert.Equal(t, 2, p.NumBlocks())

	p = Piece{Length: 2*16*1024 + 42}
	assert.Equal(t, 3, p.NumBlocks())
}

func TestGetBlock(t *testing.T) {
	p := Piece{
		Index:  1,
		Length: 2 * 16 * 1024,
	}

	_, ok := p.GetBlock(2)
	assert.False(t, ok)

	b, ok := p.GetBlock(0)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 0, Begin: 0, Length: 16 * 1024}, b)

	b, ok = p.GetBlock(1)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 1, Begin: 16 * 1024, Length: 16 * 1024}, b)

	p = Piece{
		Index:  1,
		Length: 2*16*1024 + 42,
	}

	_, ok = p.GetBlock(3)
	assert.False(t, ok)

	b, ok = p.GetBlock(0)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 0, Begin: 0, Length: 16 * 1024}, b)

	b, ok = p.GetBlock(1)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 1, Begin: 16 * 1024, Length: 16 * 1024}, b)

	b, ok = p.GetBlock(2)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 2, Begin: 2 * 16 * 1024, Length: 42}, b)
}

func TestFindBlock(t *testing.T) {
	p := Piece{
		Index:  1,
		Length: 2*BlockSize + 42,
	}

	_, ok := p.FindBlock(55, BlockSize)
	assert.False(t, ok)

	_, ok = p.FindBlock(3*BlockSize, BlockSize)
	assert.False(t, ok)

	_, ok = p.FindBlock(0, 1234)
	assert.False(t, ok)

	b, ok := p.FindBlock(0, BlockSize)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 0, Begin: 0, Length: BlockSize}, b)

	b, ok = p.FindBlock(BlockSize, BlockSize)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 1, Begin: BlockSize, Length: BlockSize}, b)

	b, ok = p.FindBlock(2*BlockSize, 42)
	assert.True(t, ok)
	assert.Equal(t, Block{Index: 2, Begin: 2 * BlockSize, Length: 42}, b)
}
