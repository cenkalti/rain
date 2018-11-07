package pexlist

import (
	"net"
	"strings"

	"github.com/cenkalti/rain/torrent/internal/tracker"
)

const (
	// BEP 11: Except for the initial PEX message the combined amount of added v4/v6 contacts should not exceed 50 entries.
	// The same applies to dropped entries.
	maxPeers = 50
	// TODO PEX send recent seen list if not enough peers
	// BEP 11: For filling underpopulated lists
	maxRecent = 25
)

type PEXList struct {
	added   map[tracker.CompactPeer]struct{}
	dropped map[tracker.CompactPeer]struct{}
	flushed bool
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

func (l *PEXList) Flush() (added, dropped string) {
	added = l.flush(l.added, l.flushed)
	dropped = l.flush(l.dropped, l.flushed)
	l.flushed = true
	return
}

func (l *PEXList) flush(m map[tracker.CompactPeer]struct{}, limit bool) string {
	count := len(m)
	if limit && count > maxPeers {
		count = maxPeers
	}

	var s strings.Builder
	s.Grow(count * 6)
	for p := range m {
		if count == 0 {
			break
		}
		count--

		b, err := p.MarshalBinary()
		if err != nil {
			panic(err)
		}
		s.Write(b)
		delete(m, p)
	}
	return s.String()
}
