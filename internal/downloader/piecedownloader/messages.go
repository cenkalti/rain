package piecedownloader

import (
	"github.com/cenkalti/rain/internal/piece"
)

type Piece struct {
	Block *piece.Block
	Data  []byte
}
