package piecepicker

import (
	"testing"

	"github.com/cenkalti/rain/internal/piece"
	"github.com/stretchr/testify/assert"
)

func TestPickLastPieceOfSmallestGap(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pieces[1].Done = true
	peer := newPeer(0)
	pp := New(pieces, 2, nil)
	pp.maxWebseedPieces = 10
	assert.Nil(t, pp.pickLastPieceOfSmallestGap(peer))
}

func TestFindGapsContiguous(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		p := newPiece(i)
		pieces[i] = p
	}
	pp := New(pieces, 2, nil)
	pp.maxWebseedPieces = 10
	assert.Equal(t, []Range{{0, 10}}, pp.findGaps())
}

func TestFindGapsSplit(t *testing.T) {
	pieces := make([]piece.Piece, 9)
	for i := range pieces {
		p := newPiece(i)
		pieces[i] = p
	}
	pieces[4].Done = true
	pp := New(pieces, 2, nil)
	pp.maxWebseedPieces = 10
	assert.Equal(t, []Range{{0, 4}, {5, 9}}, pp.findGaps())
}

func TestFindGapsLimitMaxPieces(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		p := newPiece(i)
		pieces[i] = p
	}
	pp := New(pieces, 2, nil)
	pp.maxWebseedPieces = 4
	assert.Equal(t, []Range{{0, 4}, {4, 8}, {8, 10}}, pp.findGaps())
}
