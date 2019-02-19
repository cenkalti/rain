package piecewriter

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceWriter struct {
	Piece  *piece.Piece
	Peer   *peer.Peer
	Buffer bufferpool.Buffer

	HashOK bool
	Error  error
}

func New(p *piece.Piece, pe *peer.Peer, buf bufferpool.Buffer) *PieceWriter {
	return &PieceWriter{
		Piece:  p,
		Peer:   pe,
		Buffer: buf,
	}
}

func (w *PieceWriter) Run(resultC chan *PieceWriter, closeC chan struct{}) {
	w.HashOK = w.Piece.VerifyHash(w.Buffer.Data, sha1.New()) // nolint: gosec
	if w.HashOK {
		_, w.Error = w.Piece.Data.Write(w.Buffer.Data)
	}
	select {
	case resultC <- w:
	case <-closeC:
	}
}
