package addrlist

import (
	"net"
	"testing"

	"github.com/cenkalti/rain/internal/peersource"
	"github.com/stretchr/testify/assert"
)

func TestAddrList(t *testing.T) {
	clientIP := net.IPv4(1, 2, 3, 4)
	al := New(2, nil, 5000, &clientIP)

	// Push 1st addr
	al.Push([]*net.TCPAddr{newAddr("1.1.1.1")}, peersource.Tracker)
	assert.Equal(t, len(al.peerByTime), 1)
	assert.Equal(t, al.peerByPriority.Len(), 1)
	assert.Equal(t, al.peerByTime[0].index, 0)

	// Push same addr again
	al.Push([]*net.TCPAddr{newAddr("1.1.1.1")}, peersource.Tracker)
	assert.Equal(t, len(al.peerByTime), 1)
	assert.Equal(t, al.peerByPriority.Len(), 1)
	assert.Equal(t, al.peerByTime[0].index, 0)

	// Push 2nd addr
	al.Push([]*net.TCPAddr{newAddr("2.2.2.2")}, peersource.Tracker)
	assert.Equal(t, len(al.peerByTime), 2)
	assert.Equal(t, al.peerByPriority.Len(), 2)
	assert.Equal(t, al.peerByTime[0].index, 0)
	assert.Equal(t, al.peerByTime[1].index, 1)

	// Pop an addr
	al.Pop()
	assert.Equal(t, len(al.peerByTime), 2)
	assert.Equal(t, al.peerByPriority.Len(), 1)
	assert.Equal(t, al.peerByTime[1], (*peerAddr)(nil))
	assert.Equal(t, al.peerByTime[0].index, 0)

	// Push 3nd addr
	al.Push([]*net.TCPAddr{newAddr("3.3.3.3")}, peersource.Tracker)
	assert.Equal(t, len(al.peerByTime), 2)
	assert.Equal(t, al.peerByPriority.Len(), 2)
	assert.Equal(t, al.peerByTime[0].index, 0)
	assert.Equal(t, al.peerByTime[1].index, 1)

	// Push 4nd addr
	al.Push([]*net.TCPAddr{newAddr("4.4.4.4")}, peersource.Tracker)
	assert.Equal(t, len(al.peerByTime), 2)
	assert.Equal(t, al.peerByPriority.Len(), 2)
	assert.Equal(t, al.peerByTime[0].index, 0)
	assert.Equal(t, al.peerByTime[1].index, 1)
}

func newAddr(ip string) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 1}
}
