package peer

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
	Index, Begin uint32
}

type Choke struct{}
