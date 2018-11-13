package piecewriter

import (
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type PieceWriter struct {
	Piece *pieceio.Piece
	Error error

	closeC chan struct{}
	doneC  chan struct{}
}

func New(p *pieceio.Piece) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (w *PieceWriter) Close() {
	close(w.closeC)
	<-w.doneC
}

func (w *PieceWriter) Run(data []byte, resultC chan *PieceWriter) {
	defer close(w.doneC)

	_, w.Error = w.Piece.Data.Write(data)
	select {
	case resultC <- w:
	case <-w.closeC:
	}
}
