package peerreader

import (
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/peerprotocol"
)

// Piece message that is read from peers.
// Data of the piece is wrapped with a bufferpool.Buffer object.
type Piece struct {
	peerprotocol.PieceMessage
	Buffer bufferpool.Buffer
}
