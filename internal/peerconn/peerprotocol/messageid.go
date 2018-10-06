package peerprotocol

import "strconv"

// MessageID is identifier for messages sent between peers.
type MessageID uint8

// Peer message types
const (
	Choke MessageID = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
	Port
	Suggest     = 13
	HaveAll     = 14
	HaveNone    = 15
	Reject      = 16
	AllowedFast = 17
	Extension   = 20
)

var messageIDStrings = map[MessageID]string{
	0:  "choke",
	1:  "unchoke",
	2:  "interested",
	3:  "not interested",
	4:  "have",
	5:  "bitfield",
	6:  "request",
	7:  "piece",
	8:  "cancel",
	9:  "port",
	13: "suggest",
	14: "have all",
	15: "have none",
	16: "reject",
	17: "allowed fast",
	20: "extension",
}

func (m MessageID) String() string {
	s, ok := messageIDStrings[m]
	if !ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return s
}
