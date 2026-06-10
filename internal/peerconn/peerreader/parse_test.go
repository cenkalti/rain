package peerreader

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestReader starts a PeerReader on one end of an in-memory pipe and returns
// the reader plus the peer's end of the connection for writing raw bytes.
func newTestReader(t *testing.T) (*PeerReader, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	r := New(server, logger.New("test"), time.Minute, 1<<20, nil)
	go r.Run()
	t.Cleanup(func() {
		r.Stop()        // unblock a pending send on the messages channel
		client.Close()  // unblock a pending read
		<-r.Done()
	})
	return r, client
}

// receive waits for the next parsed message from the reader.
func receive(t *testing.T, r *PeerReader) any {
	t.Helper()
	select {
	case msg := <-r.Messages():
		return msg
	case <-r.Done():
		t.Fatal("reader stopped before delivering a message")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a message")
	}
	return nil
}

func TestReaderParsesEmptyMessage(t *testing.T) {
	r, client := newTestReader(t)
	go func() { _, _ = client.Write(header(peerprotocol.Unchoke, 0)) }()

	_, ok := receive(t, r).(peerprotocol.UnchokeMessage)
	assert.True(t, ok)
}

func TestReaderParsesHave(t *testing.T) {
	r, client := newTestReader(t)
	body := []byte{0, 0, 0, 42}
	go func() {
		_, _ = client.Write(header(peerprotocol.Have, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	hm, ok := msg.(peerprotocol.HaveMessage)
	require.True(t, ok, "expected HaveMessage, got %T", msg)
	assert.Equal(t, uint32(42), hm.Index)
}

func TestReaderParsesRequest(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], 1)      // index
	binary.BigEndian.PutUint32(body[4:8], 2)      // begin
	binary.BigEndian.PutUint32(body[8:12], 16384) // length, at MaxBlockSize
	go func() {
		_, _ = client.Write(header(peerprotocol.Request, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	rm, ok := msg.(peerprotocol.RequestMessage)
	require.True(t, ok, "expected RequestMessage, got %T", msg)
	assert.Equal(t, uint32(1), rm.Index)
	assert.Equal(t, uint32(2), rm.Begin)
	assert.Equal(t, uint32(16384), rm.Length)
}

// TestReaderParsesAllowedFast guards the read side of the fast-extension path,
// complementing the writer-side framing test.
func TestReaderParsesAllowedFast(t *testing.T) {
	r, client := newTestReader(t)
	body := []byte{0, 0, 0, 9}
	go func() {
		_, _ = client.Write(header(peerprotocol.AllowedFast, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	am, ok := msg.(peerprotocol.AllowedFastMessage)
	require.True(t, ok, "expected AllowedFastMessage, got %T", msg)
	assert.Equal(t, uint32(9), am.Index)
}

func TestReaderSkipsKeepAlive(t *testing.T) {
	r, client := newTestReader(t)
	go func() {
		_, _ = client.Write([]byte{0, 0, 0, 0}) // keep-alive: zero-length message
		_, _ = client.Write(header(peerprotocol.Interested, 0))
	}()

	_, ok := receive(t, r).(peerprotocol.InterestedMessage)
	assert.True(t, ok)
}

func TestReaderDiscardsUnknownMessage(t *testing.T) {
	r, client := newTestReader(t)
	go func() {
		// Unknown message id with a 3-byte body that must be discarded, followed
		// by a known message that should still be delivered.
		_, _ = client.Write(header(peerprotocol.MessageID(50), 3))
		_, _ = client.Write([]byte{1, 2, 3})
		_, _ = client.Write(header(peerprotocol.NotInterested, 0))
	}()

	_, ok := receive(t, r).(peerprotocol.NotInterestedMessage)
	assert.True(t, ok)
}
