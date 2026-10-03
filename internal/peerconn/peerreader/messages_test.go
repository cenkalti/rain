package peerreader

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/juju/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/bencode"
)

// expectStopped asserts the reader gave up rather than delivering a message.
// A malformed or hostile message must drop the connection, not be forwarded.
func expectStopped(t *testing.T, r *PeerReader) {
	t.Helper()
	select {
	case <-r.Done():
	case msg := <-r.Messages():
		t.Fatalf("reader delivered %T instead of stopping", msg)
	case <-time.After(5 * time.Second):
		t.Fatal("reader neither stopped nor delivered a message")
	}
}

// The empty messages carry no body, so the whole message is its header. They
// are grouped because each one is a one-line case in the parser and a missing
// entry would silently become a discarded "unknown" message.
func TestReaderParsesBodilessMessages(t *testing.T) {
	cases := []struct {
		id   peerprotocol.MessageID
		want any
	}{
		{peerprotocol.Choke, peerprotocol.ChokeMessage{}},
		{peerprotocol.Unchoke, peerprotocol.UnchokeMessage{}},
		{peerprotocol.Interested, peerprotocol.InterestedMessage{}},
		{peerprotocol.NotInterested, peerprotocol.NotInterestedMessage{}},
		{peerprotocol.HaveAll, peerprotocol.HaveAllMessage{}},
		{peerprotocol.HaveNone, peerprotocol.HaveNoneMessage{}},
	}
	for _, c := range cases {
		t.Run(c.id.String(), func(t *testing.T) {
			r, client := newTestReader(t)
			go func() { _, _ = client.Write(header(c.id, 0)) }()
			assert.Equal(t, c.want, receive(t, r))
		})
	}
}

func TestReaderParsesReject(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], 7)
	binary.BigEndian.PutUint32(body[4:8], 8)
	binary.BigEndian.PutUint32(body[8:12], 9)
	go func() {
		_, _ = client.Write(header(peerprotocol.Reject, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	rm, ok := msg.(peerprotocol.RejectMessage)
	require.True(t, ok, "expected RejectMessage, got %T", msg)
	assert.Equal(t, uint32(7), rm.Index)
	assert.Equal(t, uint32(8), rm.Begin)
	assert.Equal(t, uint32(9), rm.Length)
}

func TestReaderParsesCancel(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], 3)
	binary.BigEndian.PutUint32(body[4:8], 4)
	binary.BigEndian.PutUint32(body[8:12], 5)
	go func() {
		_, _ = client.Write(header(peerprotocol.Cancel, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	cm, ok := msg.(peerprotocol.CancelMessage)
	require.True(t, ok, "expected CancelMessage, got %T", msg)
	assert.Equal(t, uint32(3), cm.Index)
	assert.Equal(t, uint32(4), cm.Begin)
	assert.Equal(t, uint32(5), cm.Length)
}

func TestReaderParsesPort(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, 6881)
	go func() {
		_, _ = client.Write(header(peerprotocol.Port, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	pm, ok := msg.(peerprotocol.PortMessage)
	require.True(t, ok, "expected PortMessage, got %T", msg)
	assert.Equal(t, uint16(6881), pm.Port)
}

func TestReaderParsesPiece(t *testing.T) {
	r, client := newTestReader(t)
	block := []byte("abcdefgh")
	body := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(body[0:4], 11) // index
	binary.BigEndian.PutUint32(body[4:8], 22) // begin
	copy(body[8:], block)
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	pm, ok := msg.(Piece)
	require.True(t, ok, "expected Piece, got %T", msg)
	assert.Equal(t, uint32(11), pm.Index)
	assert.Equal(t, uint32(22), pm.Begin)
	assert.Equal(t, block, pm.Buffer.Data)
	pm.Buffer.Release()
}

func TestReaderParsesZeroLengthPiece(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 8)
	binary.BigEndian.PutUint32(body[0:4], 1)
	binary.BigEndian.PutUint32(body[4:8], 0)
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	pm, ok := msg.(Piece)
	require.True(t, ok, "expected Piece, got %T", msg)
	assert.Empty(t, pm.Buffer.Data)
	pm.Buffer.Release()
}

// A request for more than MaxBlockSize is refused: serving it would let a peer
// pull an arbitrary amount of data per message.
func TestReaderRejectsOversizedRequest(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 12)
	binary.BigEndian.PutUint32(body[0:4], 1)
	binary.BigEndian.PutUint32(body[4:8], 0)
	binary.BigEndian.PutUint32(body[8:12], MaxBlockSize+1)
	go func() {
		_, _ = client.Write(header(peerprotocol.Request, len(body)))
		_, _ = client.Write(body)
	}()

	expectStopped(t, r)
}

// Likewise a piece bigger than one block, which would otherwise size a buffer
// from an attacker-controlled length.
//
// The index/begin pair has to be sent: the parser reads those 8 bytes before it
// checks the remaining length, so the block-size guard sits after them, not
// before. Only the block payload is withheld here.
func TestReaderRejectsOversizedPiece(t *testing.T) {
	r, client := newTestReader(t)
	pieceHeader := make([]byte, 8)
	binary.BigEndian.PutUint32(pieceHeader[0:4], 1)
	binary.BigEndian.PutUint32(pieceHeader[4:8], 0)
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, 8+piece.BlockSize+1))
		_, _ = client.Write(pieceHeader)
	}()

	expectStopped(t, r)
}

func TestReaderAcceptsPieceAtBlockSize(t *testing.T) {
	r, client := newTestReader(t)
	body := make([]byte, 8+piece.BlockSize)
	binary.BigEndian.PutUint32(body[0:4], 1)
	binary.BigEndian.PutUint32(body[4:8], 0)
	body[8] = 0xAA
	body[len(body)-1] = 0xBB
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	pm, ok := msg.(Piece)
	require.True(t, ok, "expected Piece, got %T", msg)
	require.Len(t, pm.Buffer.Data, piece.BlockSize)
	assert.Equal(t, byte(0xAA), pm.Buffer.Data[0])
	assert.Equal(t, byte(0xBB), pm.Buffer.Data[piece.BlockSize-1])
	pm.Buffer.Release()
}

func TestReaderParsesExtensionHandshake(t *testing.T) {
	r, client := newTestReader(t)

	payload, err := bencode.EncodeBytes(peerprotocol.ExtensionHandshakeMessage{
		M:            map[string]uint8{"ut_metadata": 2},
		V:            "rain test",
		MetadataSize: 1234,
	})
	require.NoError(t, err)
	body := append([]byte{peerprotocol.ExtensionIDHandshake}, payload...)

	go func() {
		_, _ = client.Write(header(peerprotocol.Extension, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	hm, ok := msg.(peerprotocol.ExtensionHandshakeMessage)
	require.True(t, ok, "expected ExtensionHandshakeMessage, got %T", msg)
	assert.Equal(t, "rain test", hm.V)
	assert.Equal(t, 1234, hm.MetadataSize)
	assert.Equal(t, uint8(2), hm.M["ut_metadata"])
}

// An unknown extension id is an error from UnmarshalBinary, which must drop the
// connection rather than forward a nil payload.
func TestReaderRejectsUnknownExtensionID(t *testing.T) {
	r, client := newTestReader(t)
	body := []byte{99, 'd', 'e'} // id 99 is not a registered extension
	go func() {
		_, _ = client.Write(header(peerprotocol.Extension, len(body)))
		_, _ = client.Write(body)
	}()

	expectStopped(t, r)
}

func TestReaderRejectsMalformedExtensionPayload(t *testing.T) {
	r, client := newTestReader(t)
	body := []byte{peerprotocol.ExtensionIDHandshake, 'n', 'o', 't', 'b', 'e', 'n', 'c'}
	go func() {
		_, _ = client.Write(header(peerprotocol.Extension, len(body)))
		_, _ = client.Write(body)
	}()

	expectStopped(t, r)
}

// A message whose body never arrives must end the read loop instead of leaving
// the reader wedged.
func TestReaderStopsOnTruncatedBody(t *testing.T) {
	cases := []struct {
		name string
		id   peerprotocol.MessageID
		body int
		send []byte
	}{
		{"have", peerprotocol.Have, 4, []byte{0, 0}},
		{"request", peerprotocol.Request, 12, []byte{0, 0, 0, 1}},
		{"cancel", peerprotocol.Cancel, 12, []byte{0, 0, 0, 1}},
		{"reject", peerprotocol.Reject, 12, []byte{0, 0, 0, 1}},
		{"allowed fast", peerprotocol.AllowedFast, 4, []byte{0}},
		{"port", peerprotocol.Port, 2, []byte{0}},
		{"bitfield", peerprotocol.Bitfield, 10, []byte{1, 2, 3}},
		{"piece", peerprotocol.Piece, 8 + 100, []byte{0, 0, 0, 1, 0, 0, 0, 0, 1, 2}},
		{"extension", peerprotocol.Extension, 20, []byte{peerprotocol.ExtensionIDHandshake}},
		{"unknown id", peerprotocol.MessageID(77), 10, []byte{1, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, client := newTestReader(t)
			go func() {
				_, _ = client.Write(header(c.id, c.body))
				_, _ = client.Write(c.send)
				client.Close() // no more data is coming
			}()
			expectStopped(t, r)
		})
	}
}

// A truncated length prefix is the degenerate case: fewer than four bytes then
// the connection closes.
func TestReaderStopsOnTruncatedLengthPrefix(t *testing.T) {
	r, client := newTestReader(t)
	go func() {
		_, _ = client.Write([]byte{0, 0})
		client.Close()
	}()
	expectStopped(t, r)
}

func TestReaderDeliversMessagesInOrder(t *testing.T) {
	r, client := newTestReader(t)
	go func() {
		_, _ = client.Write(header(peerprotocol.Choke, 0))
		_, _ = client.Write(header(peerprotocol.Unchoke, 0))
		_, _ = client.Write(header(peerprotocol.Interested, 0))
	}()

	assert.IsType(t, peerprotocol.ChokeMessage{}, receive(t, r))
	assert.IsType(t, peerprotocol.UnchokeMessage{}, receive(t, r))
	assert.IsType(t, peerprotocol.InterestedMessage{}, receive(t, r))
}

// These build the reader by hand rather than with newTestReader, whose cleanup
// also calls Stop: stopC is closed, not signalled, so a second Stop panics.

// Stop is only observed where the loop selects on stopC, which is the message
// handoff. A reader parked on a socket read does not see it — that is why
// callers close the connection as well, and why the shutdown order in
// newTestReader matters.
func TestStopAloneDoesNotInterruptAnIdleRead(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	r := New(server, logger.New("test"), time.Minute, 1<<20, nil)
	go r.Run()

	r.Stop()
	select {
	case <-r.Done():
		t.Fatal("read loop ended without the connection being closed")
	case <-time.After(200 * time.Millisecond):
		// Still parked in the read, as expected.
	}

	// Closing the connection is what actually unblocks it.
	client.Close()
	select {
	case <-r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("closing the connection did not end the read loop")
	}
}

// With a message parsed and waiting to be handed over, the loop is sitting on
// the select, so Stop ends it immediately.
func TestStopUnblocksPendingMessageDelivery(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	r := New(server, logger.New("test"), time.Minute, 1<<20, nil)
	go r.Run()

	// Nobody reads Messages(), so the loop blocks trying to deliver this.
	go func() { _, _ = client.Write(header(peerprotocol.Choke, 0)) }()
	time.Sleep(100 * time.Millisecond)

	r.Stop()
	select {
	case <-r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end a loop blocked on message delivery")
	}
}

func TestClosingConnectionEndsTheReadLoop(t *testing.T) {
	server, client := net.Pipe()

	r := New(server, logger.New("test"), time.Minute, 1<<20, nil)
	go r.Run()

	client.Close()
	select {
	case <-r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("closing the connection did not end the read loop")
	}
}

// The rate limiter sits inside the piece read: the reader asks the bucket how
// long to wait for the block's worth of tokens, then sleeps that long before
// reading. A generous bucket must not change what arrives.
func TestPieceReadThroughRateLimiter(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	bucket := ratelimit.NewBucketWithRate(1<<20, 1<<20) // 1 MiB/s, full
	r := New(server, logger.New("test"), time.Minute, 1<<20, bucket)
	go r.Run()
	t.Cleanup(func() {
		r.Stop()
		client.Close()
		<-r.Done()
	})

	block := []byte("rate limited")
	body := make([]byte, 8+len(block))
	binary.BigEndian.PutUint32(body[0:4], 5)
	binary.BigEndian.PutUint32(body[4:8], 6)
	copy(body[8:], block)
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, len(body)))
		_, _ = client.Write(body)
	}()

	msg := receive(t, r)
	pm, ok := msg.(Piece)
	require.True(t, ok, "expected Piece, got %T", msg)
	assert.Equal(t, block, pm.Buffer.Data)
	pm.Buffer.Release()
}

// While waiting on an exhausted bucket the reader does watch stopC, so Stop
// ends the read there even though it cannot interrupt a plain socket read.
func TestStopWhileWaitingForRateLimiter(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	// One byte per second with an empty burst: the wait for a block is long
	// enough that the test controls when the read ends.
	bucket := ratelimit.NewBucketWithRate(1, 1)
	bucket.TakeAvailable(1)

	r := New(server, logger.New("test"), time.Minute, 1<<20, bucket)
	go r.Run()

	body := make([]byte, 8+1024)
	binary.BigEndian.PutUint32(body[0:4], 1)
	binary.BigEndian.PutUint32(body[4:8], 0)
	go func() {
		_, _ = client.Write(header(peerprotocol.Piece, len(body)))
		_, _ = client.Write(body)
	}()

	// Give the reader time to reach the bucket wait, then release it.
	time.Sleep(200 * time.Millisecond)
	r.Stop()

	select {
	case <-r.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not interrupt the rate-limiter wait")
	}
}
