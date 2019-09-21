package pexlist

import (
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRecentlySeen(t *testing.T) {
	var l RecentlySeen
	assert.Equal(t, 0, l.Len())
	l.Add(newAddr("1.1.1.1"))
	assert.Equal(t, 1, l.Len())
	l.Add(newAddr("1.1.1.1"))
	assert.Equal(t, 1, l.Len())
	for i := 0; i < 24; i++ {
		l.Add(newAddr("2.2.2." + strconv.Itoa(i)))
	}
	assert.Equal(t, 25, l.Len())
	l.Add(newAddr("3.3.3.3"))
	assert.Equal(t, 25, l.Len())
}

func newAddr(ip string) *net.TCPAddr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: 1}
}
