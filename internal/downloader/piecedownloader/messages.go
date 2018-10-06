package piecedownloader

import (
	"github.com/cenkalti/rain/internal/pieceio"
)

type Piece struct {
	Block *pieceio.Block
	Data  []byte
}
