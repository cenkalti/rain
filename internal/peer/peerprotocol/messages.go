package peerprotocol

type HaveMessage struct {
	Index uint32
}

type RequestMessage struct {
	Index, Begin, Length uint32
}

type PieceMessage struct {
	Index, Begin uint32
}
