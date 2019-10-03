package pieceset

import (
	"github.com/cenkalti/rain/internal/piece"
)

// PieceSet is a slice of Piece with methods for operating on that slice.
type PieceSet struct {
	Pieces []*piece.Piece
}

// Add the piece to the set.
func (l *PieceSet) Add(pe *piece.Piece) bool {
	for _, p := range l.Pieces {
		if p == pe {
			return false
		}
	}
	l.Pieces = append(l.Pieces, pe)
	return true
}

// Remove the piece from the set.
func (l *PieceSet) Remove(pe *piece.Piece) bool {
	for i, p := range l.Pieces {
		if p == pe {
			l.Pieces[i] = l.Pieces[len(l.Pieces)-1]
			l.Pieces = l.Pieces[:len(l.Pieces)-1]
			return true
		}
	}
	return false
}

// Has returns true if the set contains the piece.
func (l *PieceSet) Has(pe *piece.Piece) bool {
	for _, p := range l.Pieces {
		if p == pe {
			return true
		}
	}
	return false
}

// Len returns the number of pieces in the set.
func (l *PieceSet) Len() int {
	return len(l.Pieces)
}
