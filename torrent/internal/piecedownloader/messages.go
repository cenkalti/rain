package piecedownloader

import (
	"net"

	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type piece struct {
	Block *pieceio.Block
	Conn  net.Conn
	DoneC chan struct{}
}
