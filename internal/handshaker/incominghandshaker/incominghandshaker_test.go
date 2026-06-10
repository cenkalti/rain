package incominghandshaker_test

import (
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/v2/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	peerID  = [20]byte{1}
	ourID   = [20]byte{2}
	ourExt  = [8]byte{0x0B}
	peerExt = [8]byte{0x0A}

	wantedInfoHash = [20]byte{0xE}
	peerInfoHash   = [20]byte{0xF} // different from wantedInfoHash
)

// TestIncomingHandshakeSucceeds runs the incoming handshaker against a real
// outgoing peer over loopback and checks it learns the peer's id/extensions.
func TestIncomingHandshakeSucceeds(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)

	oh := outgoinghandshaker.New(addr, peersource.Tracker)
	outResultC := make(chan *outgoinghandshaker.OutgoingHandshaker, 1)
	go oh.Run(5*time.Second, 5*time.Second, peerID, wantedInfoHash, outResultC, peerExt, true, false)

	conn, err := l.Accept()
	require.NoError(t, err)
	ih := incominghandshaker.New(conn)
	inResultC := make(chan *incominghandshaker.IncomingHandshaker, 1)
	checkInfoHash := func(h [20]byte) bool { return h == wantedInfoHash }
	go ih.Run(ourID, nil, checkInfoHash, inResultC, 5*time.Second, ourExt, false)

	outRes := <-outResultC
	inRes := <-inResultC

	require.NoError(t, inRes.Error)
	require.NoError(t, outRes.Error)
	assert.Equal(t, peerID, inRes.PeerID)
	assert.Equal(t, peerExt, inRes.Extensions)
	require.NotNil(t, inRes.Conn)

	outRes.Conn.Close()
	inRes.Conn.Close()
}

// TestIncomingHandshakeRejectsUnknownInfoHash checks that the incoming
// handshaker fails when the peer offers an info hash we are not serving.
func TestIncomingHandshakeRejectsUnknownInfoHash(t *testing.T) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer l.Close()
	addr := l.Addr().(*net.TCPAddr)

	oh := outgoinghandshaker.New(addr, peersource.Tracker)
	outResultC := make(chan *outgoinghandshaker.OutgoingHandshaker, 1)
	go oh.Run(5*time.Second, 5*time.Second, peerID, peerInfoHash, outResultC, peerExt, true, false)

	conn, err := l.Accept()
	require.NoError(t, err)
	ih := incominghandshaker.New(conn)
	inResultC := make(chan *incominghandshaker.IncomingHandshaker, 1)
	// We only serve wantedInfoHash, so the peer's peerInfoHash is rejected.
	checkInfoHash := func(h [20]byte) bool { return h == wantedInfoHash }
	go ih.Run(ourID, nil, checkInfoHash, inResultC, 5*time.Second, ourExt, false)

	inRes := <-inResultC
	require.Error(t, inRes.Error)

	// Close the accepted conn so the dangling outgoing handshake returns at once
	// instead of waiting for its handshake timeout, then drain its result.
	conn.Close()
	<-outResultC
}
