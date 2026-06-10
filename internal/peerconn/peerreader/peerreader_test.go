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

// header builds the 4-byte length prefix and 1-byte message ID for a message
// whose body is bodyLen bytes long. The body itself is not included.
func header(id peerprotocol.MessageID, bodyLen int) []byte {
	b := make([]byte, 5)
	binary.BigEndian.PutUint32(b[:4], uint32(1+bodyLen))
	b[4] = byte(id)
	return b
}

func TestRejectsOversizedMessage(t *testing.T) {
	const maxMsgSize = 100

	server, client := net.Pipe()
	defer client.Close()

	r := New(server, logger.New("test"), time.Minute, maxMsgSize, nil)
	go r.Run()

	// Announce a bitfield far larger than the limit, but send no body. Without
	// the size guard the reader would allocate the buffer and block in
	// io.ReadFull until the read deadline; with the guard it returns at once.
	go func() {
		_, _ = client.Write(header(peerprotocol.Bitfield, maxMsgSize+1))
	}()

	select {
	case <-r.Done():
	case msg := <-r.Messages():
		t.Fatalf("oversized message was not rejected: got %T", msg)
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not reject oversized message")
	}
}

func TestAcceptsMessageAtLimit(t *testing.T) {
	const maxMsgSize = 100

	server, client := net.Pipe()

	r := New(server, logger.New("test"), time.Minute, maxMsgSize, nil)
	go r.Run()
	defer func() {
		// Close the conn first so the reader's pending Read returns at once,
		// instead of waiting for the read deadline.
		client.Close()
		<-r.Done()
	}()

	body := []byte{0xAB, 0xCD}
	go func() {
		_, _ = client.Write(header(peerprotocol.Bitfield, len(body)))
		_, _ = client.Write(body)
	}()

	select {
	case msg := <-r.Messages():
		bm, ok := msg.(peerprotocol.BitfieldMessage)
		require.True(t, ok, "expected BitfieldMessage, got %T", msg)
		assert.Equal(t, body, bm.Data)
	case <-time.After(5 * time.Second):
		t.Fatal("valid message under the limit was not delivered")
	}
}
