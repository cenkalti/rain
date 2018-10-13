package peerreader

import (
	"net"

	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Conn   net.Conn
	Length uint32
	Done   chan struct{}
}

func newPiece(msg peerprotocol.PieceMessage, conn net.Conn, length uint32) *Piece {
	return &Piece{
		PieceMessage: msg,
		Conn:         conn,
		Length:       length,
		Done:         make(chan struct{}),
	}
}
