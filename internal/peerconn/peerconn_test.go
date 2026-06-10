package peerconn

import (
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestConn wraps one end of an in-memory pipe in a running Conn and ensures
// it is closed when the test finishes.
func newTestConn(t *testing.T, nc net.Conn) *Conn {
	t.Helper()
	c := New(nc, logger.New("test"), time.Minute, 10, 1<<20, true, nil, nil)
	go c.Run()
	t.Cleanup(c.Close)
	return c
}

func recvMessage(t *testing.T, c *Conn) any {
	t.Helper()
	select {
	case msg := <-c.Messages():
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a message")
	}
	return nil
}

// TestConnRoundTrip wires two Conns back-to-back over net.Pipe and checks that
// a message sent on one is framed, read, parsed, and delivered on the other —
// exercising the reader+writer composition inside Conn.Run.
func TestConnRoundTrip(t *testing.T) {
	nc1, nc2 := net.Pipe()
	c1 := newTestConn(t, nc1)
	c2 := newTestConn(t, nc2)

	// c1 -> c2
	c1.SendMessage(peerprotocol.HaveMessage{Index: 5})
	msg := recvMessage(t, c2)
	hm, ok := msg.(peerprotocol.HaveMessage)
	require.True(t, ok, "expected HaveMessage, got %T", msg)
	assert.Equal(t, uint32(5), hm.Index)

	// c2 -> c1
	c2.SendMessage(peerprotocol.UnchokeMessage{})
	_, ok = recvMessage(t, c1).(peerprotocol.UnchokeMessage)
	assert.True(t, ok)
}
