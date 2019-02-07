package peerreader

import (
	"github.com/cenkalti/rain/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Data   []byte
	buffer []byte
}

func (p *Piece) ReleaseBuffer() {
	blockPool.Put(p.buffer)
}
