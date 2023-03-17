package piecewriter

import (
	"crypto/sha1"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/rcrowley/go-metrics"
)

// PieceWriter writes the data in the buffer to disk.
type PieceWriter struct {
	Piece  *piece.Piece
	Source any
	Buffer bufferpool.Buffer

	HashOK bool
	Error  error

	PerFileBytes map[string]int64
}

// New returns new PieceWriter for a given piece.
func New(p *piece.Piece, source any, buf bufferpool.Buffer) *PieceWriter {
	return &PieceWriter{
		Piece:        p,
		Source:       source,
		Buffer:       buf,
		PerFileBytes: make(map[string]int64),
	}
}

// Run checks the hash, then writes the data in the buffer to the disk.
func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}, writesPerSecond, writeBytesPerSecond metrics.Meter, sem *semaphore.Semaphore) {
	w.HashOK = w.Piece.VerifyHash(w.Buffer.Data, sha1.New())
	if w.HashOK {
		writesPerSecond.Mark(1)
		writeBytesPerSecond.Mark(int64(len(w.Buffer.Data)))
		sem.Wait()
		_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
		sem.Signal()

		for _, sec := range w.Piece.Data {
			if !sec.Padding {
				w.PerFileBytes[sec.Name] += sec.Length
			}
		}
	}
	select {
	case resultC <- w:
	case <-closeC:
	}
}
