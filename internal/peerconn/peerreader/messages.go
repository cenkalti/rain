package peerreader

import (
	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Buffer bufferpool.Buffer
}
