package piecedownloader

import (
	"testing"

	"github.com/cenkalti/rain/internal/bufferpool"
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
	}
	pe := &TestPeer{}
	d := New(pi, pe, false, buf)
	assert.Equal(t, 11, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 0, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 7, len(d.remaining))
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
	assert.Equal(t, 7, len(d.remaining))
	assert.Equal(t, 4, len(d.pending))
	assert.Equal(t, 0, len(d.done))
	assert.False(t, d.Done())
	assert.Equal(t, []Message{
		{Index: 1, Begin: 0 * blockSize, Length: blockSize},
		{Index: 1, Begin: 1 * blockSize, Length: blockSize},
		{Index: 1, Begin: 2 * blockSize, Length: blockSize},
		{Index: 1, Begin: 3 * blockSize, Length: blockSize},
	}, pe.requested)

	assert.Nil(t, d.GotBlock(piece.Block{Index: 0, Begin: 0, Length: blockSize}, make([]byte, blockSize)))
	assert.Equal(t, 4, len(pe.requested))
	assert.Equal(t, 7, len(d.remaining))
	assert.Equal(t, 3, len(d.pending))
	assert.Equal(t, 1, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 6, len(d.remaining))
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

	assert.Nil(t, d.GotBlock(piece.Block{Index: 1, Begin: 1 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 2, Begin: 2 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 3, Begin: 3 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 4, Begin: 4 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Equal(t, 6, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 5, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(4)
	assert.Equal(t, 2, len(d.remaining))
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

	assert.Nil(t, d.GotBlock(piece.Block{Index: 5, Begin: 5 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Equal(t, 2, len(d.remaining))
	assert.Equal(t, 3, len(d.pending))
	assert.Equal(t, 6, len(d.done))
	assert.False(t, d.Done())

	d.Choked()
	assert.Equal(t, 5, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 6, len(d.done))
	assert.False(t, d.Done())

	d.RequestBlocks(99)
	assert.Nil(t, d.GotBlock(piece.Block{Index: 6, Begin: 6 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 7, Begin: 7 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 8, Begin: 8 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 9, Begin: 9 * blockSize, Length: blockSize}, make([]byte, blockSize)))
	assert.Nil(t, d.GotBlock(piece.Block{Index: 10, Begin: 10 * blockSize, Length: 42}, make([]byte, 42)))
	assert.Equal(t, 0, len(d.remaining))
	assert.Equal(t, 0, len(d.pending))
	assert.Equal(t, 11, len(d.done))
	assert.True(t, d.Done())
}
