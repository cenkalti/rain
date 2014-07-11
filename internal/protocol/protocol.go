package protocol

import (
	"crypto/sha1"
	"encoding/hex"
)

type InfoHash [sha1.Size]byte

func (i InfoHash) String() string { return hex.EncodeToString(i[:]) }

type PeerID [20]byte

func (p PeerID) String() string { return hex.EncodeToString(p[:]) }
