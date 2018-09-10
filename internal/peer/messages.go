package peer

import (
	"github.com/cenkalti/rain/internal/bitfield"
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
	HaveAll       chan *Peer
	Bitfield      chan Bitfield
	Request       chan Request
	Piece         chan Piece
	Reject        chan Request
	AllowedFast   chan Have
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
		HaveAll:       make(chan *Peer),
		Bitfield:      make(chan Bitfield),
		Request:       make(chan Request),
		Piece:         make(chan Piece),
		Reject:        make(chan Request),
		AllowedFast:   make(chan Have),
		// Cancel:        make(chan Cancel),
	}
}

type Have struct {
	Peer  *Peer
	Piece *piece.Piece
}

type Bitfield struct {
	Peer     *Peer
	Bitfield *bitfield.Bitfield
}

type Request struct {
	Peer   *Peer
	Piece  *piece.Piece
	Begin  uint32
	Length uint32
}

type Piece struct {
	Peer  *Peer
	Piece *piece.Piece
	Block *piece.Block
	Data  []byte
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
