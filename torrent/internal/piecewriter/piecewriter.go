package piecewriter

import (
	"github.com/cenkalti/rain/torrent/internal/piece"
)

type PieceWriter struct {
	Piece  *piece.Piece
	Buffer []byte
	Lenght uint32
	Error  error

	closeC chan struct{}
	doneC  chan struct{}
}

func New(p *piece.Piece, buf []byte, length uint32) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Buffer: buf,
		Lenght: length,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (w *PieceWriter) Close() {
	close(w.closeC)
	<-w.doneC
}

func (w *PieceWriter) Run(resultC chan *PieceWriter) {
	defer close(w.doneC)

	_, w.Error = w.Piece.Data.Write(w.Buffer[:w.Lenght])
	select {
	case resultC <- w:
	case <-w.closeC:
	}
}
