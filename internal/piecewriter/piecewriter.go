package piecewriter

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

type PieceWriter struct {
	Piece  *piece.Piece
	Source interface{}
	Buffer bufferpool.Buffer

	HashOK bool
	Error  error
}

func New(p *piece.Piece, source interface{}, buf bufferpool.Buffer) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Source: source,
		Buffer: buf,
	}
}

func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	w.HashOK = w.Piece.VerifyHash(w.Buffer.Data, sha1.New()) // nolint: gosec
	if w.HashOK {
		writesPerSecond.Mark(1)
		writeBytesPerSecond.Mark(int64(len(w.Buffer.Data)))
		sem.Wait()
		_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
		sem.Signal()
	}
	select {
	case resultC <- w:
	case <-closeC:
	}
}
