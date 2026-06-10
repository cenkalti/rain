package outgoinghandshaker_test

import (
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/v2/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/v2/internal/mse"
	"github.com/cenkalti/rain/v2/internal/peersource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	outID    = [20]byte{1}
	inID     = [20]byte{2}
	outExt   = [8]byte{0x0A}
	inExt    = [8]byte{0x0B}
	infoHash = [20]byte{0x0E}
)

// TestHandshakeRoundTrip drives a real outgoing handshaker against a real
// incoming handshaker over a loopback TCP connection, for both the plaintext
// and the encrypted (MSE/RC4) paths. It verifies that each side ends up with
// the other's peer id, extensions, the negotiated cipher, and a usable conn.
func TestHandshakeRoundTrip(t *testing.T) {
	cases := []struct {
		name          string
		disableOutEnc bool
		forceOutEnc   bool
		wantCipher    mse.CryptoMethod
	}{
		{"plaintext", true, false, 0},
		{"encrypted", false, true, mse.RC4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
			require.NoError(t, err)
			defer l.Close()
			addr := l.Addr().(*net.TCPAddr)

			oh := outgoinghandshaker.New(addr, peersource.Tracker)
			outResultC := make(chan *outgoinghandshaker.OutgoingHandshaker, 1)
			go oh.Run(5*time.Second, 5*time.Second, outID, infoHash, outResultC, outExt, tc.disableOutEnc, tc.forceOutEnc)

			conn, err := l.Accept()
			require.NoError(t, err)
			ih := incominghandshaker.New(conn)
			inResultC := make(chan *incominghandshaker.IncomingHandshaker, 1)
			getSKey := func(h [20]byte) []byte {
				if h == mse.HashSKey(infoHash[:]) {
					return infoHash[:]
				}
				return nil
			}
			checkInfoHash := func(h [20]byte) bool { return h == infoHash }
			go ih.Run(inID, getSKey, checkInfoHash, inResultC, 5*time.Second, inExt, false)

			outRes := <-outResultC
			inRes := <-inResultC

			require.NoError(t, outRes.Error)
			require.NoError(t, inRes.Error)

			// Each side learns the other's identity and extension bits.
			assert.Equal(t, inID, outRes.PeerID)
			assert.Equal(t, outID, inRes.PeerID)
			assert.Equal(t, inExt, outRes.Extensions)
			assert.Equal(t, outExt, inRes.Extensions)

			assert.Equal(t, tc.wantCipher, outRes.Cipher)
			assert.Equal(t, tc.wantCipher, inRes.Cipher)

			require.NotNil(t, outRes.Conn)
			require.NotNil(t, inRes.Conn)
			outRes.Conn.Close()
			inRes.Conn.Close()
		})
	}
}
