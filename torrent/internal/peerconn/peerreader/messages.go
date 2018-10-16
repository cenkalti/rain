package peerreader

import (
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Data []byte
}
