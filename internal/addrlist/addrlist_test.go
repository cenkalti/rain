package addrlist

import (
	"net"
	"testing"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/stretchr/testify/assert"
)

func TestAddrList(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4)
	al := New(2, nil, 5000, &clientIP)

	// Push 1st addr
	al.Push([]*net.TCPAddr{newAddr("1.1.1.1")}, peer.SourceTracker)
	assert.Equal(t, al.peers.Len(), 1)

	// Push same addr again
	al.Push([]*net.TCPAddr{newAddr("1.1.1.1")}, peer.SourceTracker)
	assert.Equal(t, al.peers.Len(), 1)

	// Push 2nd addr
	al.Push([]*net.TCPAddr{newAddr("2.2.2.2")}, peer.SourceTracker)
	assert.Equal(t, al.peers.Len(), 2)

	// Pop an addr
	al.Pop()
	assert.Equal(t, al.peers.Len(), 1)

	// Push 3nd addr
	al.Push([]*net.TCPAddr{newAddr("3.3.3.3")}, peer.SourceTracker)
	assert.Equal(t, al.peers.Len(), 2)

	// Push 4nd addr
	al.Push([]*net.TCPAddr{newAddr("4.4.4.4")}, peer.SourceTracker)
	assert.Equal(t, al.peers.Len(), 2)
}

func newAddr(ip string) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 1}
}
