package peer

import (
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

type Messages struct {
	Connect       chan *Peer
	Disconnect    chan *Peer
	Choke         chan *Peer
	Unchoke       chan *Peer
	Interested    chan *Peer
	NotInterested chan *Peer
	HaveAll       chan *Peer
	Have          chan Have
	AllowedFast   chan Have
	Bitfield      chan Bitfield
	Request       chan Request
	Reject        chan Request
	Piece         chan Piece
	// TODO handle cancel message
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
		HaveAll:       make(chan *Peer),
		Have:          make(chan Have),
		AllowedFast:   make(chan Have),
		Bitfield:      make(chan Bitfield),
		Request:       make(chan Request),
		Reject:        make(chan Request),
		Piece:         make(chan Piece),
		// TODO handle cancel message
		// Cancel:        make(chan Cancel),
	}
}

type Have struct {
	Peer *Peer
	peerprotocol.HaveMessage
}

type Bitfield struct {
	Peer *Peer
	Data []byte
}

type Request struct {
	Peer *Peer
	peerprotocol.RequestMessage
}

type Piece struct {
	Peer *Peer
	peerprotocol.PieceMessage
	Data []byte
}
