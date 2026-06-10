package peerprotocol

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/bencode"
)

func TestTruncateIP(t *testing.T) {
	t.Run("ipv4", func(t *testing.T) {
		ip := net.ParseIP("1.2.3.4")
		require.NotNil(t, ip)
		assert.Len(t, truncateIP(ip), net.IPv4len)
	})
	t.Run("ipv4-mapped ipv6", func(t *testing.T) {
		ip := net.ParseIP("::ffff:1.2.3.4")
		require.NotNil(t, ip)
		got := truncateIP(ip)
		assert.Len(t, got, net.IPv4len)
		assert.True(t, net.IPv4(1, 2, 3, 4).Equal(got))
	})
	t.Run("ipv6", func(t *testing.T) {
		ip := net.ParseIP("2001:db8::1")
		require.NotNil(t, ip)
		assert.Len(t, truncateIP(ip), net.IPv6len)
	})
}

func TestNewExtensionHandshake(t *testing.T) {
	hs := NewExtensionHandshake(1234, "rain/test", net.ParseIP("1.2.3.4"), 50)

	assert.Equal(t, uint8(ExtensionIDMetadata), hs.M[ExtensionKeyMetadata])
	assert.Equal(t, uint8(ExtensionIDPEX), hs.M[ExtensionKeyPEX])
	assert.Equal(t, "rain/test", hs.V)
	assert.Equal(t, 1234, hs.MetadataSize)
	assert.Equal(t, 50, hs.RequestQueue)
	assert.Equal(t, string(net.IPv4(1, 2, 3, 4).To4()), hs.YourIP)
}

func TestExtensionMessageHandshakeRoundTrip(t *testing.T) {
	hs := NewExtensionHandshake(99, "rain", net.ParseIP("1.2.3.4"), 25)
	msg := ExtensionMessage{ExtendedMessageID: ExtensionIDHandshake, Payload: hs}

	var buf bytes.Buffer
	_, err := msg.WriteTo(&buf)
	require.NoError(t, err)

	var got ExtensionMessage
	require.NoError(t, got.UnmarshalBinary(buf.Bytes()))
	assert.Equal(t, uint8(ExtensionIDHandshake), got.ExtendedMessageID)

	payload, ok := got.Payload.(ExtensionHandshakeMessage)
	require.True(t, ok)
	assert.Equal(t, "rain", payload.V)
	assert.Equal(t, 99, payload.MetadataSize)
	assert.Equal(t, 25, payload.RequestQueue)
	assert.Equal(t, uint8(ExtensionIDMetadata), payload.M[ExtensionKeyMetadata])
	assert.Equal(t, uint8(ExtensionIDPEX), payload.M[ExtensionKeyPEX])
}

func TestExtensionMessageHandshakeClampsNegative(t *testing.T) {
	// A peer may advertise negative metadata_size / reqq; both are clamped to 0.
	body := map[string]any{
		"m":             map[string]any{ExtensionKeyMetadata: ExtensionIDMetadata},
		"v":             "buggy-peer",
		"metadata_size": -5,
		"reqq":          -1,
	}
	enc, err := bencode.EncodeBytes(body)
	require.NoError(t, err)
	data := append([]byte{ExtensionIDHandshake}, enc...)

	var got ExtensionMessage
	require.NoError(t, got.UnmarshalBinary(data))
	payload, ok := got.Payload.(ExtensionHandshakeMessage)
	require.True(t, ok)
	assert.Equal(t, 0, payload.MetadataSize)
	assert.Equal(t, 0, payload.RequestQueue)
}

func TestExtensionMessageMetadataRoundTrip(t *testing.T) {
	data := []byte("piece-data-payload")
	mm := ExtensionMetadataMessage{
		Type:      ExtensionMetadataMessageTypeData,
		Piece:     3,
		TotalSize: 12345,
		Data:      data,
	}
	msg := ExtensionMessage{ExtendedMessageID: ExtensionIDMetadata, Payload: mm}

	var buf bytes.Buffer
	_, err := msg.WriteTo(&buf)
	require.NoError(t, err)

	var got ExtensionMessage
	require.NoError(t, got.UnmarshalBinary(buf.Bytes()))
	payload, ok := got.Payload.(ExtensionMetadataMessage)
	require.True(t, ok)
	assert.Equal(t, ExtensionMetadataMessageTypeData, payload.Type)
	assert.Equal(t, uint32(3), payload.Piece)
	assert.Equal(t, 12345, payload.TotalSize)
	// The trailing bytes after the bencoded header are the piece data.
	assert.Equal(t, data, payload.Data)
}

func TestExtensionMessagePEXRoundTrip(t *testing.T) {
	pm := ExtensionPEXMessage{Added: "added-peers", Dropped: "dropped-peers"}
	msg := ExtensionMessage{ExtendedMessageID: ExtensionIDPEX, Payload: pm}

	var buf bytes.Buffer
	_, err := msg.WriteTo(&buf)
	require.NoError(t, err)

	var got ExtensionMessage
	require.NoError(t, got.UnmarshalBinary(buf.Bytes()))
	payload, ok := got.Payload.(ExtensionPEXMessage)
	require.True(t, ok)
	assert.Equal(t, "added-peers", payload.Added)
	assert.Equal(t, "dropped-peers", payload.Dropped)
}

func TestExtensionMessageUnmarshalErrors(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		// No bytes: the extended message id cannot be read.
		var got ExtensionMessage
		assert.Error(t, got.UnmarshalBinary(nil))
	})
	t.Run("unknown extension id", func(t *testing.T) {
		var got ExtensionMessage
		assert.Error(t, got.UnmarshalBinary([]byte{99}))
	})
}
