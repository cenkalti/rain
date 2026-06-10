package peer_test

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peer"
	"github.com/cenkalti/rain/v2/internal/peerconn"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 5 * time.Second

// harness wires a peer.Peer to a remote peerconn.Conn over an in-memory pipe so
// that messages can be driven into the peer and observed coming out of it.
type harness struct {
	peer       *peer.Peer
	remote     *peerconn.Conn
	remoteConn net.Conn
	messages   chan peer.Message
	pieces     chan peer.PieceMessage
	snubbed    chan *peer.Peer
	disconnect chan *peer.Peer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	peerConn, remoteConn := net.Pipe()

	var ext [8]byte
	p := peer.New(peerConn, peersource.Incoming, [20]byte{1}, ext, 0, time.Minute, 100*time.Millisecond, 10, 1<<20, nil, nil)
	remote := peerconn.New(remoteConn, logger.New("remote"), time.Minute, 10, 1<<20, true, nil, nil)

	h := &harness{
		peer:       p,
		remote:     remote,
		remoteConn: remoteConn,
		messages:   make(chan peer.Message, 16),
		pieces:     make(chan peer.PieceMessage, 16),
		snubbed:    make(chan *peer.Peer, 4),
		disconnect: make(chan *peer.Peer, 4),
	}
	go p.Run(h.messages, h.pieces, h.snubbed, h.disconnect)
	go remote.Run()

	t.Cleanup(func() {
		p.Close()
		// Closing the underlying conn lets the remote peerconn.Run exit on its
		// own; closing remote again would double-close its close channel.
		_ = remoteConn.Close()
	})
	return h
}

func TestPeerForwardsMessages(t *testing.T) {
	h := newHarness(t)

	h.remote.SendMessage(peerprotocol.InterestedMessage{})

	select {
	case m := <-h.messages:
		_, ok := m.Message.(peerprotocol.InterestedMessage)
		assert.True(t, ok, "expected InterestedMessage, got %T", m.Message)
		assert.Same(t, h.peer, m.Peer)
	case <-time.After(testTimeout):
		t.Fatal("peer did not forward the message")
	}
}

func TestPeerForwardsPieces(t *testing.T) {
	h := newHarness(t)

	data := []byte("0123456789ABCDEF")
	h.remote.SendPiece(peerprotocol.RequestMessage{Index: 4, Begin: 0, Length: uint32(len(data))}, bytes.NewReader(data))

	select {
	case pm := <-h.pieces:
		assert.Same(t, h.peer, pm.Peer)
		assert.Equal(t, uint32(4), pm.Piece.Index)
		assert.Equal(t, data, pm.Piece.Buffer.Data)
		pm.Piece.Buffer.Release()
	case <-time.After(testTimeout):
		t.Fatal("peer did not forward the piece")
	}
}

func TestPeerChokeUnchokeSendMessages(t *testing.T) {
	h := newHarness(t)

	h.peer.Unchoke()
	assert.False(t, h.peer.Choking())
	requireRemoteMessage[peerprotocol.UnchokeMessage](t, h)

	h.peer.Choke()
	assert.True(t, h.peer.Choking())
	requireRemoteMessage[peerprotocol.ChokeMessage](t, h)
}

func TestPeerRequestPieceSendsMessage(t *testing.T) {
	h := newHarness(t)

	h.peer.RequestPiece(2, 16, 32)
	msg := requireRemoteMessageAny(t, h)
	rm, ok := msg.(peerprotocol.RequestMessage)
	require.True(t, ok, "expected RequestMessage, got %T", msg)
	assert.Equal(t, peerprotocol.RequestMessage{Index: 2, Begin: 16, Length: 32}, rm)
}

func TestPeerDisconnectsWhenRemoteCloses(t *testing.T) {
	h := newHarness(t)

	// Simulate the remote peer hanging up.
	require.NoError(t, h.remoteConn.Close())

	select {
	case dp := <-h.disconnect:
		assert.Same(t, h.peer, dp)
	case <-time.After(testTimeout):
		t.Fatal("peer did not report disconnect")
	}
}

func TestPeerSnubTimer(t *testing.T) {
	h := newHarness(t)

	h.peer.ResetSnubTimer() // harness uses a 100ms snub timeout

	select {
	case sp := <-h.snubbed:
		assert.Same(t, h.peer, sp)
	case <-time.After(testTimeout):
		t.Fatal("snub timer did not fire")
	}
}

func TestPeerClientFallsBackToPeerID(t *testing.T) {
	h := newHarness(t)
	// Without an extension handshake, Client() returns the asciified peer id.
	assert.NotEmpty(t, h.peer.Client())
}

// requireRemoteMessage asserts that the next message the remote receives is of
// type T.
func requireRemoteMessage[T any](t *testing.T, h *harness) {
	t.Helper()
	msg := requireRemoteMessageAny(t, h)
	_, ok := msg.(T)
	assert.True(t, ok, "expected %T, got %T", *new(T), msg)
}

func requireRemoteMessageAny(t *testing.T, h *harness) any {
	t.Helper()
	select {
	case msg := <-h.remote.Messages():
		return msg
	case <-time.After(testTimeout):
		t.Fatal("remote did not receive a message")
		return nil
	}
}
