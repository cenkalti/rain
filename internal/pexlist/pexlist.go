package pexlist

import (
	"net"
	"strings"

	"github.com/cenkalti/rain/internal/tracker"
)

const (
	// BEP 11: Except for the initial PEX message the combined amount of added v4/v6 contacts should not exceed 50 entries.
	// The same applies to dropped entries.
	maxPeers = 50
)

// PEXList contains the list of peer address for sending them to a peer at certain interval.
// List contains 2 separate lists for added and dropped addresses.
type PEXList struct {
	added   map[tracker.CompactPeer]struct{}
	dropped map[tracker.CompactPeer]struct{}
	flushed bool
}

// New returns a new empty PEXList.
func New() *PEXList {
	return &PEXList{
		added:   make(map[tracker.CompactPeer]struct{}),
		dropped: make(map[tracker.CompactPeer]struct{}),
	}
}

// NewWithRecentlySeen returns a new PEXList with given peers added to the dropped part.
func NewWithRecentlySeen(rs []tracker.CompactPeer) *PEXList {
	l := New()
	for _, cp := range rs {
		l.dropped[cp] = struct{}{}
	}
	return l
}

// Add adds the address to the added part and removes from dropped part.
func (l *PEXList) Add(addr *net.TCPAddr) {
	p := tracker.NewCompactPeer(addr)
	l.added[p] = struct{}{}
	delete(l.dropped, p)
}

// Drop adds the address to the dropped part and removes from added part.
func (l *PEXList) Drop(addr *net.TCPAddr) {
	peer := tracker.NewCompactPeer(addr)
	l.dropped[peer] = struct{}{}
	delete(l.added, peer)
}

// Flush returns added and dropped parts and empty the list.
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
