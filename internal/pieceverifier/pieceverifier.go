package pieceverifier

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceVerifier struct {
	Piece  *piece.Piece
	Peer   *peer.Peer
	Buffer []byte
	Length uint32
	OK     bool
}

func New(p *piece.Piece, pe *peer.Peer, buf []byte, length uint32) *PieceVerifier {
	return &PieceVerifier{
		Piece:  p,
		Peer:   pe,
		Buffer: buf,
		Length: length,
	}
}

func (v *PieceVerifier) Run(resultC chan *PieceVerifier, closeC chan struct{}) {
	data := v.Buffer[:v.Length]
	v.OK = v.Piece.VerifyHash(data, sha1.New()) // nolint: gosec

	select {
	case resultC <- v:
	case <-closeC:
	}
}
