package piecedownloader

import (
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	Block *pieceio.Block
	Data  []byte
}
