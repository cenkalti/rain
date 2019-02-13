package pieceset

import (
	"github.com/cenkalti/rain/internal/piece"
)

type PieceSet struct {
	Pieces []*piece.Piece
}

func (l *PieceSet) Add(pe *piece.Piece) bool {
	for _, p := range l.Pieces {
		if p == pe {
			return false
		}
	}
	l.Pieces = append(l.Pieces, pe)
	return true
}

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

func (l *PieceSet) Has(pe *piece.Piece) bool {
	for _, p := range l.Pieces {
		if p == pe {
			return true
		}
	}
	return false
}

func (l *PieceSet) Len() int {
	return len(l.Pieces)
}
