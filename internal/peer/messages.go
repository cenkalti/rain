package peer

import (
	"github.com/cenkalti/rain/internal/piece"
)

type Messages struct {
	Connect       chan *Peer
	Disconnect    chan *Peer
	Choke         chan *Peer
	Unchoke       chan *Peer
	Interested    chan *Peer
	NotInterested chan *Peer
	Have          chan Have
	Request       chan Request
	Piece         chan Piece
	// Bitfield      chan Bitfield
	// Cancel        chan Cancel
}

func NewMessages() *Messages {
	return &Messages{
		Connect:       make(chan *Peer),
		Disconnect:    make(chan *Peer),
		Choke:         make(chan *Peer),
		Unchoke:       make(chan *Peer),
		Interested:    make(chan *Peer),
		NotInterested: make(chan *Peer),
		Have:          make(chan Have),
		Request:       make(chan Request),
		Piece:         make(chan Piece),
		// Bitfield:      make(chan Bitfield),
		// Cancel:        make(chan Cancel),
	}
}

type Have struct {
	*Peer
	Index uint32
}

type Request struct {
	*Peer
	Index, Begin, Length uint32
}

type Piece struct {
	*Peer
	*piece.Piece
	*piece.Block
	Data []byte
}

type haveMessage struct {
	Index uint32
}

type requestMessage struct {
	Index, Begin, Length uint32
}

type pieceMessage struct {
	Index, Begin uint32
}
