package protocol

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strconv"
)

const (
	// Request pieces in blocks of this size.
	BlockSize = 16 * 1024
	// Reject requests larger than this size.
	MaxAllowedBlockSize = 32 * 1024
)

// InfoHash is the sha1 hash of "info" dictionary in torrent file.
type InfoHash [sha1.Size]byte

// NewInfoHashString returns a new InfoHash value from a string.
// s must be 40 (hex encoded) or 32 (base32 encoded) characters, otherwise it returns error.
func NewInfoHashString(s string) (InfoHash, error) {
	var ih InfoHash
	var b []byte
	var err error
	if len(s) == 40 {
		b, err = hex.DecodeString(s)
	} else if len(s) == 32 {
		b, err = base32.StdEncoding.DecodeString(s)
	} else {
		return ih, errors.New("info hash must be 32 or 40 characters")
	}
	if err != nil {
		return ih, err
	}
	copy(ih[:], b)
	return ih, nil
}

// String returns the hex represenation of i.
func (i InfoHash) String() string { return hex.EncodeToString(i[:]) }

// MarshalJSON marshals i as 40 characters hex string.
func (i InfoHash) MarshalJSON() ([]byte, error) { return []byte(`"` + i.String() + `"`), nil }

// PeerID is unique identifier for the client.
type PeerID [20]byte

// String returns the hex representation of p.
func (p PeerID) String() string { return hex.EncodeToString(p[:]) }

// MessageType is identifier for messages sent between peers.
type MessageType uint8

// Peer message types
const (
	Choke MessageType = iota
	Unchoke
	Interested
	NotInterested
	Have
	Bitfield
	Request
	Piece
	Cancel
	Port
	Extension = 20
)

var messageStrings = map[MessageType]string{
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

func (m MessageType) String() string {
	s, ok := messageStrings[m]
	if !ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return s
}
