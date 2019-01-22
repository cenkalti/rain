package peerreader

import (
	"github.com/cenkalti/rain/session/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Data []byte
}
