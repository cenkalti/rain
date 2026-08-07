package piecepicker

import (
	"testing"

	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/stretchr/testify/assert"
)

func TestPickLastPieceOfSmallestGap(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pieces[1].Done = true
	peer := newPeer(0)
	pp := New(pieces, 2, nil, false)
	pp.maxWebseedPieces = 10
	assert.Nil(t, pp.pickLastPieceOfSmallestGap(peer))
}

func TestFindGapsContiguous(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		p := newPiece(i)
		pieces[i] = p
	}
	pp := New(pieces, 2, nil, false)
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
	pp := New(pieces, 2, nil, false)
	pp.maxWebseedPieces = 10
	assert.Equal(t, []Range{{0, 4}, {5, 9}}, pp.findGaps())
}

func TestFindPieceRangeForWebseedSequential(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		pieces[i] = newPiece(i)
	}
	pieces[2].Done = true
	pp := New(pieces, 2, nil, true)
	pp.maxWebseedPieces = 10
	// Gaps are {0,2} and {3,10}.
	assert.Equal(t, []Range{{0, 2}, {3, 10}}, pp.findGaps())
	// Sequential mode downloads the last piece first.
	assert.Equal(t, &Range{9, 10}, pp.findPieceRangeForWebseed())
	// Then it takes the first gap, not the largest one.
	pieces[9].Done = true
	assert.Equal(t, &Range{0, 2}, pp.findPieceRangeForWebseed())
}

func TestFindGapsLimitMaxPieces(t *testing.T) {
	pieces := make([]piece.Piece, 10)
	for i := range pieces {
		p := newPiece(i)
		pieces[i] = p
	}
	pp := New(pieces, 2, nil, false)
	pp.maxWebseedPieces = 4
	assert.Equal(t, []Range{{0, 4}, {4, 8}, {8, 10}}, pp.findGaps())
}
