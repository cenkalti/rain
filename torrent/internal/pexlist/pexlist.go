package pexlist

import (
	"net"
	"strings"

	"github.com/cenkalti/rain/torrent/internal/tracker"
)

const (
	maxPeers  = 50
	maxRecent = 25
)

type PEXList struct {
	added   map[tracker.CompactPeer]struct{}
	dropped map[tracker.CompactPeer]struct{}
}

func New() *PEXList {
	return &PEXList{
		added:   make(map[tracker.CompactPeer]struct{}),
		dropped: make(map[tracker.CompactPeer]struct{}),
	}
}

func (l *PEXList) Add(addr *net.TCPAddr) {
	p := tracker.NewCompactPeer(addr)
	l.added[p] = struct{}{}
	delete(l.dropped, p)
}

func (l *PEXList) Drop(addr *net.TCPAddr) {
	peer := tracker.NewCompactPeer(addr)
	l.dropped[peer] = struct{}{}
	delete(l.added, peer)
}

func (l *PEXList) Clear() {
	l.added = make(map[tracker.CompactPeer]struct{})
	l.dropped = make(map[tracker.CompactPeer]struct{})
}

func (l *PEXList) Flush() (added, dropped string) {
	return l.flush(l.added), l.flush(l.dropped)
}

func (l *PEXList) flush(m map[tracker.CompactPeer]struct{}) string {
	var s strings.Builder
	s.Grow(len(m) * 6)
	for p := range m {
		b, err := p.MarshalBinary()
		if err != nil {
			panic(err)
		}
		s.Write(b)
		delete(m, p)
	}
	return s.String()
}
