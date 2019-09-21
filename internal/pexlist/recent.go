package pexlist

import (
	"net"

	"github.com/cenkalti/rain/internal/tracker"
)

const MaxLength = 25

type RecentlySeen struct {
	peers  []tracker.CompactPeer
	offset int
	length int
}

func (l *RecentlySeen) Add(addr *net.TCPAddr) {
	cp := tracker.NewCompactPeer(addr)
	if l.has(cp) {
		return
	}
	if l.length >= MaxLength {
		l.peers[l.offset] = cp
	} else {
		l.peers = append(l.peers, cp)
		l.length++
	}
	l.offset = (l.offset + 1) % MaxLength
}

func (l *RecentlySeen) has(cp tracker.CompactPeer) bool {
	for _, p := range l.peers {
		if p == cp {
			return true
		}
	}
	return false
}

func (l *RecentlySeen) Peers() []tracker.CompactPeer {
	return l.peers
}

func (l *RecentlySeen) Len() int {
	return len(l.peers)
}
