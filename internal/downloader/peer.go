package downloader

import (
	"github.com/cenkalti/rain/internal/peer"
)

type Peer struct {
	*peer.Peer
	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool
}
