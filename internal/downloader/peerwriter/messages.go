package peerwriter

import (
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type Request struct {
	Piece   *piece.Piece
	Request peer.Request
}
