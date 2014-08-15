package protocol

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
)

type InfoHash [sha1.Size]byte

func NewInfoHashString(s string) (InfoHash, error) {
	var ih InfoHash
	if len(s) != 2*sha1.Size {
		return ih, errors.New("info hash must be 40 characters")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return ih, err
	}
	copy(ih[:], b)
	return ih, nil
}

func (i InfoHash) String() string { return hex.EncodeToString(i[:]) }

type PeerID [20]byte

func (p PeerID) String() string { return hex.EncodeToString(p[:]) }

const PstrLen = 19

var Pstr = []byte("BitTorrent protocol")

type MessageType byte

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
)

var messageStrings = [...]string{
	"choke",
	"unchoke",
	"interested",
	"not interested",
	"have",
	"bitfield",
	"request",
	"piece",
	"cancel",
	"port",
}

func (m MessageType) String() string { return messageStrings[m] }
