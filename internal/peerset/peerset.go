package peerset

import (
	"github.com/cenkalti/rain/internal/peer"
)

type PeerSet struct {
	Peers []*peer.Peer
}

func (l *PeerSet) Add(pe *peer.Peer) bool {
	for _, p := range l.Peers {
		if p == pe {
			return false
		}
	}
	l.Peers = append(l.Peers, pe)
	return true
}

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

func (l *PeerSet) Has(pe *peer.Peer) bool {
	for _, p := range l.Peers {
		if p == pe {
			return true
		}
	}
	return false
}

func (l *PeerSet) Len() int {
	return len(l.Peers)
}
