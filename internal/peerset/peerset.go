package peerset

import (
	"github.com/cenkalti/rain/internal/peer"
)

// PeerSet is slice of Peers with methods operation on this slice.
type PeerSet struct {
	Peers []*peer.Peer
}

// Add new peer to the set.
func (l *PeerSet) Add(pe *peer.Peer) bool {
	for _, p := range l.Peers {
		if p == pe {
			return false
		}
	}
	l.Peers = append(l.Peers, pe)
	return true
}

// Remove peer from the set.
func (l *PeerSet) Remove(pe *peer.Peer) bool {
	for i, p := range l.Peers {
		if p == pe {
			l.Peers[i] = l.Peers[len(l.Peers)-1]
			l.Peers = l.Peers[:len(l.Peers)-1]
			return true
		}
	}
	return false
}

// Has returns true if the set contains the peer.
func (l *PeerSet) Has(pe *peer.Peer) bool {
	for _, p := range l.Peers {
		if p == pe {
			return true
		}
	}
	return false
}

// Len returns the number of items in the set.
func (l *PeerSet) Len() int {
	return len(l.Peers)
}
