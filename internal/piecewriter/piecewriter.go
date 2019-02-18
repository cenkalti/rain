package piecewriter

import (
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceWriter struct {
	Piece  *piece.Piece
	Buffer bufferpool.Buffer
	Error  error
}

func New(p *piece.Piece, buf bufferpool.Buffer) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Buffer: buf,
	}
}

func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}) {
	_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
	select {
	case resultC <- w:
	case <-closeC:
	}
}
