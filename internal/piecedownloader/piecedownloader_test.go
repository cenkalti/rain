package piecedownloader

import (
	"testing"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/filesection"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/stretchr/testify/assert"
)

type Message = peerprotocol.RequestMessage

const blockSize = piece.BlockSize

type TestPeer struct {
	requested []Message
	canceled  []Message
}

func (p *TestPeer) RequestPiece(index, begin, length uint32) {
	msg := Message{
		Index:  index,
		Begin:  begin,
		Length: length,
	}
	p.requested = append(p.requested, msg)
}

func (p *TestPeer) CancelPiece(index, begin, length uint32) {
	msg := Message{
		Index:  index,
		Begin:  begin,
		Length: length,
	}
	p.canceled = append(p.canceled, msg)
}

func (p *TestPeer) EnabledFast() bool { return false }

func TestPieceDownloader(t *testing.T) {
	bp := bufferpool.New(12 * blockSize)
	buf := bp.Get(10*blockSize + 42)
	pi := &piece.Piece{
		Index:  1,
		Length: 10*blockSize + 42,
		Data: []filesection.FileSection{
			{
				Length: 9*blockSize + 21,
			},
			{
				Length:  blockSize + 21,
				Padding: true,
			},
		},
	}
	pe := &TestPeer{}
	d := New(pi, pe, false, buf)
	assert.Equal(t, 10, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 0, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 6, len(d.remaining))
	assert.Equal(t, 4, len(d.pending))
	assert.Equal(t, 0, len(d.done))
	assert.False(t, d.Done())
	assert.Equal(t, []Message{
		{Index: 1, Begin: 0 * blockSize, Length: blockSize},
		{Index: 1, Begin: 1 * blockSize, Length: blockSize},
		{Index: 1, Begin: 2 * blockSize, Length: blockSize},
		{Index: 1, Begin: 3 * blockSize, Length: blockSize},
	}, pe.requested)

	d.RequestBlocks(4)
	assert.Equal(t, 6, len(d.remaining))
	assert.Equal(t, 4, len(d.pending))
	assert.Equal(t, 0, len(d.done))
	assert.False(t, d.Done())
	assert.Equal(t, []Message{
		{Index: 1, Begin: 0 * blockSize, Length: blockSize},
		{Index: 1, Begin: 1 * blockSize, Length: blockSize},
		{Index: 1, Begin: 2 * blockSize, Length: blockSize},
		{Index: 1, Begin: 3 * blockSize, Length: blockSize},
	}, pe.requested)

	assert.Nil(t, d.GotBlock(0, make([]byte, blockSize)))
	assert.Equal(t, 4, len(pe.requested))
	assert.Equal(t, 6, len(d.remaining))
	assert.Equal(t, 3, len(d.pending))
	assert.Equal(t, 1, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 5, len(d.remaining))
	assert.Equal(t, 4, len(d.pending))
	assert.Equal(t, 1, len(d.done))
	assert.False(t, d.Done())
	assert.Equal(t, []Message{
		{Index: 1, Begin: 0 * blockSize, Length: blockSize},
		{Index: 1, Begin: 1 * blockSize, Length: blockSize},
		{Index: 1, Begin: 2 * blockSize, Length: blockSize},
		{Index: 1, Begin: 3 * blockSize, Length: blockSize},
		{Index: 1, Begin: 4 * blockSize, Length: blockSize},
	}, pe.requested)

	assert.Nil(t, d.GotBlock(1*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(2*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(3*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(4*blockSize, make([]byte, blockSize)))
	assert.Equal(t, 5, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 5, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 1, len(d.remaining))
	assert.Equal(t, 4, len(d.pending))
	assert.Equal(t, 5, len(d.done))
	assert.False(t, d.Done())
	assert.Equal(t, []Message{
		{Index: 1, Begin: 0 * blockSize, Length: blockSize},
		{Index: 1, Begin: 1 * blockSize, Length: blockSize},
		{Index: 1, Begin: 2 * blockSize, Length: blockSize},
		{Index: 1, Begin: 3 * blockSize, Length: blockSize},
		{Index: 1, Begin: 4 * blockSize, Length: blockSize},
		{Index: 1, Begin: 5 * blockSize, Length: blockSize},
		{Index: 1, Begin: 6 * blockSize, Length: blockSize},
		{Index: 1, Begin: 7 * blockSize, Length: blockSize},
		{Index: 1, Begin: 8 * blockSize, Length: blockSize},
	}, pe.requested)

	assert.Nil(t, d.GotBlock(5*blockSize, make([]byte, blockSize)))
	assert.Equal(t, 1, len(d.remaining))
	assert.Equal(t, 3, len(d.pending))
	assert.Equal(t, 6, len(d.done))
	assert.False(t, d.Done())

	d.Choked()
	assert.Equal(t, 4, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 6, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(99)
	assert.Nil(t, d.GotBlock(6*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(7*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(8*blockSize, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(9*blockSize, make([]byte, 21)))
	assert.Equal(t, 0, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 10, len(d.done))
	assert.True(t, d.Done())
}
