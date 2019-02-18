package pieceverifier

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceVerifier struct {
	Piece  *piece.Piece
	Peer   *peer.Peer
	Buffer bufferpool.Buffer
	OK     bool
}

func New(p *piece.Piece, pe *peer.Peer, buf bufferpool.Buffer) *PieceVerifier {
	return &PieceVerifier{
		Piece:  p,
		Peer:   pe,
		Buffer: buf,
	}
}

func (v *PieceVerifier) Run(resultC chan *PieceVerifier, closeC chan struct{}) {
	v.OK = v.Piece.VerifyHash(v.Buffer.Data, sha1.New()) // nolint: gosec

	select {
	case resultC <- v:
	case <-closeC:
	}
}
