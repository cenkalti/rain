package piecewriter

import (
	"github.com/cenkalti/rain/internal/piece"
)

type PieceWriter struct {
	Piece  *piece.Piece
	Buffer []byte
	Lenght uint32
	Error  error
}

func New(p *piece.Piece, buf []byte, length uint32) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Buffer: buf,
		Lenght: length,
	}
}

func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}) {
	_, w.Error = w.Piece.Data.Write(w.Buffer[:w.Lenght])
	select {
	case resultC <- w:
	case <-closeC:
	}
}
