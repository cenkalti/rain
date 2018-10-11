package peerreader

import (
	"io"

	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

type Piece struct {
	peerprotocol.PieceMessage
	Reader io.Reader
	Length uint32
	Done   chan struct{}
}

func newPiece(msg peerprotocol.PieceMessage, r io.Reader, length uint32) *Piece {
	return &Piece{
		PieceMessage: msg,
		Reader:       r,
		Length:       length,
		Done:         make(chan struct{}),
	}
}
