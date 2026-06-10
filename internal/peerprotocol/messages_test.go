package peerprotocol

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFixedMessageRead checks that the fixed-size messages serialize their
// fields in big-endian order and report (len, io.EOF) from a single Read.
func TestFixedMessageRead(t *testing.T) {
	cases := []struct {
		name string
		msg  io.Reader
		want []byte
	}{
		{
			name: "have",
			msg:  HaveMessage{Index: 0x01020304},
			want: []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			name: "request",
			msg:  RequestMessage{Index: 1, Begin: 2, Length: 3},
			want: []byte{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3},
		},
		{
			name: "piece header",
			msg:  PieceMessage{Index: 0x0a0b0c0d, Begin: 0x11223344},
			want: []byte{0x0a, 0x0b, 0x0c, 0x0d, 0x11, 0x22, 0x33, 0x44},
		},
		{
			name: "port",
			msg:  PortMessage{Port: 0x1234},
			want: []byte{0x12, 0x34},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, len(tc.want))
			n, err := tc.msg.Read(buf)
			assert.Equal(t, len(tc.want), n)
			assert.ErrorIs(t, err, io.EOF)
			assert.Equal(t, tc.want, buf)
		})
	}
}

// TestEmptyMessageRead checks that the bodiless messages read zero bytes and
// report io.EOF.
func TestEmptyMessageRead(t *testing.T) {
	msgs := map[string]io.Reader{
		"choke":          ChokeMessage{},
		"unchoke":        UnchokeMessage{},
		"interested":     InterestedMessage{},
		"not interested": NotInterestedMessage{},
		"have all":       HaveAllMessage{},
		"have none":      HaveNoneMessage{},
	}
	for name, m := range msgs {
		t.Run(name, func(t *testing.T) {
			n, err := m.Read(nil)
			assert.Equal(t, 0, n)
			assert.ErrorIs(t, err, io.EOF)
		})
	}
}

// TestBitfieldMessageRead checks that BitfieldMessage tracks its read position
// across multiple partial reads and reports io.EOF exactly when drained.
func TestBitfieldMessageRead(t *testing.T) {
	data := []byte{0xde, 0xad, 0xbe, 0xef, 0x00}
	m := &BitfieldMessage{Data: data}

	out := make([]byte, 0, len(data))
	buf := make([]byte, 2) // smaller than data to force several reads
	for {
		n, err := m.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, data, out)
}

// TestMessageID checks that every message type reports the wire id used by the
// peer writer to frame it.
func TestMessageID(t *testing.T) {
	cases := []struct {
		msg  Message
		want MessageID
	}{
		{ChokeMessage{}, Choke},
		{UnchokeMessage{}, Unchoke},
		{InterestedMessage{}, Interested},
		{NotInterestedMessage{}, NotInterested},
		{HaveMessage{}, Have},
		{&BitfieldMessage{}, Bitfield},
		{RequestMessage{}, Request},
		{PieceMessage{}, Piece},
		{CancelMessage{}, Cancel},
		{PortMessage{}, Port},
		{HaveAllMessage{}, HaveAll},
		{HaveNoneMessage{}, HaveNone},
		{RejectMessage{}, Reject},
		{AllowedFastMessage{}, AllowedFast},
		{ExtensionMessage{}, Extension},
	}
	for _, tc := range cases {
		t.Run(tc.want.String(), func(t *testing.T) {
			assert.Equal(t, tc.want, tc.msg.ID())
		})
	}
}

// TestAllowedFastMessageID is a focused regression guard: AllowedFastMessage
// embeds HaveMessage, so without its own ID() override it would inherit Have's
// id. The peer writer derives the wire id byte from ID(), so it must report
// AllowedFast.
func TestAllowedFastMessageID(t *testing.T) {
	assert.Equal(t, MessageID(AllowedFast), AllowedFastMessage{}.ID())
	assert.NotEqual(t, MessageID(Have), AllowedFastMessage{}.ID())
}
