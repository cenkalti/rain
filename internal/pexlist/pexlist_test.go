package pexlist

import (
	"net"
	"strconv"
	"testing"

	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addr(ip string, port int) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: port}
}

// decode turns a flushed string back into addresses. Each compact peer is
// 6 bytes, so a short or ragged string is a bug in the encoder.
func decode(t *testing.T, s string) []string {
	t.Helper()
	require.Zero(t, len(s)%6, "flushed payload must be a whole number of 6-byte peers")
	addrs, err := tracker.DecodePeersCompact([]byte(s))
	require.NoError(t, err)
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func TestFlushEmptyList(t *testing.T) {
	l := New()
	added, dropped := l.Flush()
	assert.Empty(t, added)
	assert.Empty(t, dropped)
}

func TestAddThenFlush(t *testing.T) {
	l := New()
	l.Add(addr("1.2.3.4", 6881))
	l.Add(addr("5.6.7.8", 1234))

	added, dropped := l.Flush()
	assert.ElementsMatch(t, []string{"1.2.3.4:6881", "5.6.7.8:1234"}, decode(t, added))
	assert.Empty(t, dropped)
}

func TestDropThenFlush(t *testing.T) {
	l := New()
	l.Drop(addr("1.2.3.4", 6881))

	added, dropped := l.Flush()
	assert.Empty(t, added)
	assert.ElementsMatch(t, []string{"1.2.3.4:6881"}, decode(t, dropped))
}

func TestAddIsIdempotent(t *testing.T) {
	l := New()
	l.Add(addr("1.2.3.4", 6881))
	l.Add(addr("1.2.3.4", 6881))

	added, _ := l.Flush()
	assert.Len(t, decode(t, added), 1, "the same peer must not be reported twice")
}

// A peer cannot be in both halves of the message, so each operation has to
// retract the other. This is what keeps a reconnecting peer from being
// announced as added and dropped in the same flush.
func TestAddAndDropAreMutuallyExclusive(t *testing.T) {
	t.Run("drop after add", func(t *testing.T) {
		l := New()
		l.Add(addr("1.2.3.4", 6881))
		l.Drop(addr("1.2.3.4", 6881))

		added, dropped := l.Flush()
		assert.Empty(t, added)
		assert.ElementsMatch(t, []string{"1.2.3.4:6881"}, decode(t, dropped))
	})

	t.Run("add after drop", func(t *testing.T) {
		l := New()
		l.Drop(addr("1.2.3.4", 6881))
		l.Add(addr("1.2.3.4", 6881))

		added, dropped := l.Flush()
		assert.ElementsMatch(t, []string{"1.2.3.4:6881"}, decode(t, added))
		assert.Empty(t, dropped)
	})
}

func TestFlushEmptiesTheList(t *testing.T) {
	l := New()
	l.Add(addr("1.2.3.4", 6881))
	l.Drop(addr("5.6.7.8", 1234))

	added, dropped := l.Flush()
	require.Len(t, decode(t, added), 1)
	require.Len(t, decode(t, dropped), 1)

	added, dropped = l.Flush()
	assert.Empty(t, added, "a flushed peer must not be reported again")
	assert.Empty(t, dropped)
}

func TestNewWithRecentlySeenSeedsDropped(t *testing.T) {
	seen := []tracker.CompactPeer{
		tracker.NewCompactPeer(addr("1.1.1.1", 1)),
		tracker.NewCompactPeer(addr("2.2.2.2", 2)),
	}
	l := NewWithRecentlySeen(seen)

	added, dropped := l.Flush()
	assert.Empty(t, added)
	assert.ElementsMatch(t, []string{"1.1.1.1:1", "2.2.2.2:2"}, decode(t, dropped))
}

func TestAddRetractsRecentlySeenPeer(t *testing.T) {
	seen := []tracker.CompactPeer{tracker.NewCompactPeer(addr("1.1.1.1", 1))}
	l := NewWithRecentlySeen(seen)
	l.Add(addr("1.1.1.1", 1))

	added, dropped := l.Flush()
	assert.ElementsMatch(t, []string{"1.1.1.1:1"}, decode(t, added))
	assert.Empty(t, dropped, "a peer that came back must not also be reported dropped")
}

// BEP 11 caps a PEX message at 50 added and 50 dropped contacts, except for the
// initial message. The first flush is therefore unlimited and every later one
// is capped, with the overflow kept for the next flush rather than discarded.
func TestFirstFlushIsUnlimitedThenCapped(t *testing.T) {
	l := New()
	for i := range 60 {
		l.Add(addr("10.0.0."+strconv.Itoa(i), 6881))
	}

	added, _ := l.Flush()
	assert.Len(t, decode(t, added), 60, "the initial message is exempt from the cap")

	for i := range 60 {
		l.Add(addr("10.0.1."+strconv.Itoa(i), 6881))
	}
	added, _ = l.Flush()
	assert.Len(t, decode(t, added), maxPeers, "later messages are capped at 50")

	added, _ = l.Flush()
	assert.Len(t, decode(t, added), 10, "the overflow is carried over, not dropped")

	added, _ = l.Flush()
	assert.Empty(t, added)
}

func TestCapAppliesToDroppedIndependently(t *testing.T) {
	l := New()
	l.Flush() // spend the initial-message exemption

	for i := range 60 {
		l.Add(addr("10.0.0."+strconv.Itoa(i), 6881))
		l.Drop(addr("10.1.0."+strconv.Itoa(i), 6881))
	}

	added, dropped := l.Flush()
	assert.Len(t, decode(t, added), maxPeers)
	assert.Len(t, decode(t, dropped), maxPeers, "each half has its own budget")

	added, dropped = l.Flush()
	assert.Len(t, decode(t, added), 10)
	assert.Len(t, decode(t, dropped), 10)
}

// CompactPeer holds 4 bytes of address, so an IPv6-only peer flattens to
// 0.0.0.0 and any two of them collide. PEX here is effectively IPv4 only.
func TestIPv6PeersCollapseToZeroAddress(t *testing.T) {
	l := New()
	l.Add(addr("2001:db8::1", 6881))
	l.Add(addr("2001:db8::2", 6881))

	added, _ := l.Flush()
	assert.Equal(t, []string{"0.0.0.0:6881"}, decode(t, added),
		"both IPv6 peers degrade to the same zero address")
}

// An IPv4-mapped IPv6 address is a normal IPv4 peer and must survive intact.
func TestIPv4MappedAddressIsPreserved(t *testing.T) {
	l := New()
	l.Add(&net.TCPAddr{IP: net.ParseIP("::ffff:1.2.3.4"), Port: 6881})

	added, _ := l.Flush()
	assert.Equal(t, []string{"1.2.3.4:6881"}, decode(t, added))
}

func TestPortsOutsideRangeBecomeZero(t *testing.T) {
	l := New()
	l.Add(addr("1.2.3.4", 70000))

	added, _ := l.Flush()
	assert.Equal(t, []string{"1.2.3.4:0"}, decode(t, added))
}

func TestSamePeerDifferentPortsAreDistinct(t *testing.T) {
	l := New()
	l.Add(addr("1.2.3.4", 1))
	l.Add(addr("1.2.3.4", 2))

	added, _ := l.Flush()
	assert.ElementsMatch(t, []string{"1.2.3.4:1", "1.2.3.4:2"}, decode(t, added))
}
