package peer

import "strconv"

// messageID is identifier for messages sent between peers.
type messageID uint8

// Peer message types
const (
	chokeID messageID = iota
	unchokeID
	interestedID
	notInterestedID
	haveID
	bitfieldID
	requestID
	pieceID
	cancelID
	portID
	extensionID = 20
)

var messageIDStrings = map[messageID]string{
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
	20: "extension",
}

func (m messageID) String() string {
	s, ok := messageIDStrings[m]
	if !ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return s
}
