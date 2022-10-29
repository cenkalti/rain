package piece

import (
	"testing"

	"github.com/cenkalti/rain/internal/filesection"
	"github.com/stretchr/testify/assert"
)

func TestNumBlocks(t *testing.T) {
	p := Piece{Length: 2 * 16 * 1024}
	assert.Equal(t, 2, p.numBlocks())

	p = Piece{Length: 2*16*1024 + 42}
	assert.Equal(t, 3, p.numBlocks())
}

func TestFindBlock(t *testing.T) {
	p := Piece{
		Length: 2*BlockSize + 42,
		Data: []filesection.FileSection{
			{
				Length: 2*BlockSize + 42,
			},
		},
	}
	blocks := p.CalculateBlocks()
	findBlock := func(begin, length uint32) bool {
		for _, blk := range blocks {
			if blk.Begin == begin && blk.Length == length {
				return true
			}
		}
		return false
	}
	assert.False(t, findBlock(55, BlockSize))
	assert.False(t, findBlock(3*BlockSize, BlockSize))
	assert.False(t, findBlock(0, 1234))
	assert.True(t, findBlock(0, BlockSize))
	assert.True(t, findBlock(BlockSize, BlockSize))
	assert.True(t, findBlock(2*BlockSize, 42))
}

func TestCalculateBlocks(t *testing.T) {
	const blockSize = 40
	testCases := []struct {
		piece    Piece
		expected []Block
	}{
		{
			piece: Piece{
				Length: 100,
				Data: []filesection.FileSection{
					{Length: 80, Padding: false},
					{Length: 20, Padding: true},
				},
			},
			expected: []Block{
				{
					Begin:  0,
					Length: 40,
				},
				{
					Begin:  40,
					Length: 40,
				},
			},
		},
		{
			piece: Piece{
				Length: 100,
				Data: []filesection.FileSection{
					{Length: 90, Padding: false},
					{Length: 10, Padding: true},
				},
			},
			expected: []Block{
				{
					Begin:  0,
					Length: 40,
				},
				{
					Begin:  40,
					Length: 40,
				},
				{
					Begin:  80,
					Length: 10,
				},
			},
		},
		{
			piece: Piece{
				Length: 100,
				Data: []filesection.FileSection{
					{Length: 30, Padding: false},
					{Length: 60, Padding: false},
					{Length: 10, Padding: true},
				},
			},
			expected: []Block{
				{
					Begin:  0,
					Length: 40,
				},
				{
					Begin:  40,
					Length: 40,
				},
				{
					Begin:  80,
					Length: 10,
				},
			},
		},
		{
			piece: Piece{
				Length: 100,
				Data: []filesection.FileSection{
					{Length: 30, Padding: false},
					{Length: 30, Padding: false},
					{Length: 40, Padding: true},
				},
			},
			expected: []Block{
				{
					Begin:  0,
					Length: 40,
				},
				{
					Begin:  40,
					Length: 20,
				},
			},
		},
		// This case is not very realistic because it contains data after padding in the same piece.
		{
			piece: Piece{
				Length: 100,
				Data: []filesection.FileSection{
					{Length: 30, Padding: false},
					{Length: 20, Padding: true},
					{Length: 50, Padding: false},
				},
			},
			expected: []Block{
				{
					Begin:  0,
					Length: 30,
				},
				{
					Begin:  50,
					Length: 40,
				},
				{
					Begin:  90,
					Length: 10,
				},
			},
		},
	}
	for i, tc := range testCases {
		dataLen := int64(0)
		for _, sec := range tc.piece.Data {
			dataLen += sec.Length
		}
		if dataLen != int64(tc.piece.Length) {
			t.Errorf("error in test case #%d: piece and data length do not match", i)
			continue
		}
		blocks := tc.piece.calculateBlocks(blockSize)
		assert.Equal(t, tc.expected, blocks, "test case #%d", i)
	}
}
