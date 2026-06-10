package peerwriter

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/peerprotocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestWriter starts a PeerWriter on one end of an in-memory pipe and returns
// the writer plus the peer's end of the connection for reading raw bytes.
func newTestWriter(t *testing.T) (*PeerWriter, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	w := New(server, logger.New("test"), 10, true, nil)
	go w.Run()
	t.Cleanup(func() {
		w.Stop()
		client.Close()
		<-w.Done()
	})
	return w, client
}

// readFrame reads a single length-prefixed message off conn and returns its
// message id and body. It mirrors the wire framing produced by the writer:
// 4-byte big-endian length, 1-byte id, then (length-1) body bytes.
func readFrame(t *testing.T, conn net.Conn) (peerprotocol.MessageID, []byte) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	var length uint32
	require.NoError(t, binary.Read(conn, binary.BigEndian, &length))
	require.GreaterOrEqual(t, length, uint32(1), "framed message must carry at least the id byte")

	var id uint8
	require.NoError(t, binary.Read(conn, binary.BigEndian, &id))

	body := make([]byte, length-1)
	_, err := io.ReadFull(conn, body)
	require.NoError(t, err)
	return peerprotocol.MessageID(id), body
}

func TestPeerWriterFramesFixedMessage(t *testing.T) {
	w, client := newTestWriter(t)
	w.SendMessage(peerprotocol.HaveMessage{Index: 0x01020304})

	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Have, id)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, body)
}

func TestPeerWriterFramesEmptyMessage(t *testing.T) {
	w, client := newTestWriter(t)
	w.SendMessage(peerprotocol.ChokeMessage{})

	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.Choke, id)
	assert.Empty(t, body)
}

// TestPeerWriterFramesAllowedFast is the wire-level regression guard for the
// AllowedFastMessage id bug: the writer derives the id byte from msg.ID(), so a
// missing ID() override would frame the message as Have (4) instead of
// AllowedFast (17).
func TestPeerWriterFramesAllowedFast(t *testing.T) {
	w, client := newTestWriter(t)
	w.SendMessage(peerprotocol.AllowedFastMessage{HaveMessage: peerprotocol.HaveMessage{Index: 7}})

	id, body := readFrame(t, client)
	assert.Equal(t, peerprotocol.MessageID(peerprotocol.AllowedFast), id)
	assert.NotEqual(t, peerprotocol.MessageID(peerprotocol.Have), id)
	assert.Equal(t, []byte{0, 0, 0, 7}, body)
}

func TestPieceRead(t *testing.T) {
	data := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	p := Piece{
		Data: bytes.NewReader(data),
		RequestMessage: peerprotocol.RequestMessage{
			Index:  0x01020304,
			Begin:  2,
			Length: 4,
		},
	}
	buf := make([]byte, 8+4)
	n, err := p.Read(buf)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 12, n)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, buf[0:4]) // index
	assert.Equal(t, []byte{0, 0, 0, 2}, buf[4:8])             // begin
	assert.Equal(t, data[2:6], buf[8:12])                     // block read at offset begin
}

func TestPieceID(t *testing.T) {
	assert.Equal(t, peerprotocol.Piece, Piece{}.ID())
}
