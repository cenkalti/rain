package peerpriority

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPeerPriority(t *testing.T) {
	assert.EqualValues(t, 0xec2d7224, Calculate(
		newAddr("123.213.32.10"),
		newAddr("98.76.54.32"),
	))
	assert.EqualValues(t, 0xec2d7224, Calculate(
		newAddr("98.76.54.32"),
		newAddr("123.213.32.10"),
	))
	assert.Equal(t, Priority(0x99568189), Calculate(
		newAddr("123.213.32.10"),
		newAddr("123.213.32.234"),
	))
}

func newAddr(ip string) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip)}
}
