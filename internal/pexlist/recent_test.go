package pexlist

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecentlySeen(t *testing.T) {
	var l RecentlySeen
	assert.Equal(t, 0, l.Len())
	l.Add(newAddr("1.1.1.1"))
	assert.Equal(t, 1, l.Len())
	l.Add(newAddr("1.1.1.1"))
	assert.Equal(t, 1, l.Len())
	for i := range 24 {
		l.Add(newAddr("2.2.2." + strconv.Itoa(i)))
	}
	assert.Equal(t, 25, l.Len())
	l.Add(newAddr("3.3.3.3"))
	assert.Equal(t, 25, l.Len())
}

func newAddr(ip string) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 1}
}

// peerStrings renders the list so eviction order can be asserted.
func peerStrings(l *RecentlySeen) []string {
	out := make([]string, 0, l.Len())
	for _, p := range l.Peers() {
		out = append(out, p.Addr().String())
	}
	return out
}

func TestRecentlySeenZeroValueIsUsable(t *testing.T) {
	var l RecentlySeen
	assert.Equal(t, 0, l.Len())
	assert.Empty(t, l.Peers())
}

func TestRecentlySeenReturnsWhatWasAdded(t *testing.T) {
	var l RecentlySeen
	l.Add(newAddr("1.1.1.1"))
	l.Add(newAddr("2.2.2.2"))
	l.Add(newAddr("3.3.3.3"))

	assert.Equal(t, []string{"1.1.1.1:1", "2.2.2.2:1", "3.3.3.3:1"}, peerStrings(&l))
}

// Once full, the list is a ring: the next add replaces the oldest entry and
// everything else stays put.
func TestRecentlySeenEvictsOldestWhenFull(t *testing.T) {
	var l RecentlySeen
	for i := range MaxLength {
		l.Add(newAddr("10.0.0." + strconv.Itoa(i)))
	}
	require.Equal(t, MaxLength, l.Len())
	require.Equal(t, "10.0.0.0:1", peerStrings(&l)[0])

	l.Add(newAddr("9.9.9.9"))

	assert.Equal(t, MaxLength, l.Len(), "the list never grows past MaxLength")
	got := peerStrings(&l)
	assert.Equal(t, "9.9.9.9:1", got[0], "the newest entry takes the oldest slot")
	assert.NotContains(t, got, "10.0.0.0:1", "the oldest entry is gone")
	assert.Contains(t, got, "10.0.0.1:1", "the second-oldest is retained")
	assert.Contains(t, got, "10.0.0.24:1", "the most recent of the original batch is retained")
}

func TestRecentlySeenKeepsEvictingInOrder(t *testing.T) {
	var l RecentlySeen
	for i := range MaxLength {
		l.Add(newAddr("10.0.0." + strconv.Itoa(i)))
	}
	// Three more adds should consume the three oldest slots in order.
	l.Add(newAddr("9.9.9.1"))
	l.Add(newAddr("9.9.9.2"))
	l.Add(newAddr("9.9.9.3"))

	got := peerStrings(&l)
	assert.Equal(t, []string{"9.9.9.1:1", "9.9.9.2:1", "9.9.9.3:1"}, got[:3])
	assert.NotContains(t, got, "10.0.0.0:1")
	assert.NotContains(t, got, "10.0.0.1:1")
	assert.NotContains(t, got, "10.0.0.2:1")
	assert.Contains(t, got, "10.0.0.3:1")
}

// Re-adding a peer already in the list is dropped entirely, so it must not
// consume a slot or evict anything.
func TestRecentlySeenDuplicateDoesNotEvict(t *testing.T) {
	var l RecentlySeen
	for i := range MaxLength {
		l.Add(newAddr("10.0.0." + strconv.Itoa(i)))
	}
	before := peerStrings(&l)

	l.Add(newAddr("10.0.0.5"))

	assert.Equal(t, before, peerStrings(&l), "a duplicate must leave the ring untouched")
}

func TestRecentlySeenDistinguishesPorts(t *testing.T) {
	var l RecentlySeen
	l.Add(&net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1})
	l.Add(&net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 2})

	assert.Equal(t, 2, l.Len(), "the same host on another port is a different peer")
}

// The output feeds NewWithRecentlySeen, so the two have to agree on the type.
func TestRecentlySeenFeedsPEXList(t *testing.T) {
	var l RecentlySeen
	l.Add(newAddr("1.1.1.1"))
	l.Add(newAddr("2.2.2.2"))

	// Passing Peers() straight in is the type agreement being asserted.
	pl := NewWithRecentlySeen(l.Peers())
	added, dropped := pl.Flush()
	assert.Empty(t, added)
	assert.ElementsMatch(t, []string{"1.1.1.1:1", "2.2.2.2:1"}, decode(t, dropped))
}
