package protocol

import (
	"crypto/sha1"
	"encoding/hex"
)

type InfoHash [sha1.Size]byte

func (i InfoHash) String() string { return hex.EncodeToString(i[:]) }

type PeerID [20]byte

func (p PeerID) String() string { return hex.EncodeToString(p[:]) }

const PstrLen = 19

var Pstr = []byte("BitTorrent protocol")

const (
	Choke Message = iota
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

var peerMessageTypes = [...]string{
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

type Message byte

func (m Message) String() string { return peerMessageTypes[m] }
