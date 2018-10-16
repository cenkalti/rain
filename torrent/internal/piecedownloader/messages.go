package piecedownloader

import (
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type piece struct {
	Block *pieceio.Block
	Data  []byte
}
