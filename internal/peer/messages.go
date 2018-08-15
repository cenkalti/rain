package peer

import "github.com/cenkalti/rain/internal/piece"

type Message struct {
	Peer    *Peer
	Message interface{}
}

type Have struct {
	Index uint32
}

type Request struct {
	Index, Begin, Length uint32
}

type Piece struct {
	*piece.Piece
	*piece.Block
	Data []byte
}

type pieceMessage struct {
	Index, Begin uint32
}

type Choke struct{}

type Unchoke struct{}
