package connection

import (
	"bytes"
	"errors"
	"io"
	"net"
	"time"

	"github.com/cenkalti/mse"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/protocol/handshake"
)

const handshakeDeadline = 30 * time.Second

var (
	ErrInvalidInfoHash = errors.New("invalid info hash")
	ErrOwnConnection   = errors.New("dropped own connection")
	ErrNotEncrypted    = errors.New("connection is not encrypted")
)

func Dial(addr net.Addr, enableEncryption, forceEncryption bool, ourExtensions [8]byte, ih protocol.InfoHash, ourID protocol.PeerID) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID protocol.PeerID, err error) {

	log := logger.New("peer -> " + addr.String())

	// First connection
	log.Debug("Connecting to peer...")
	conn, err = net.DialTimeout(addr.Network(), addr.String(), handshakeDeadline)
	if err != nil {
		return
	}
	log.Debug("Connected")
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	out := bytes.NewBuffer(make([]byte, 0, 68))
	err = handshake.Write(out, ih, ourID, ourExtensions)
	if err != nil {
		return
	}

	if enableEncryption {
		sKey := make([]byte, 20)
		copy(sKey, ih[:])

		provide := mse.RC4
		if !forceEncryption {
			provide |= mse.PlainText
		}

		// Try encryption handshake
		encConn := mse.WrapConn(conn)
		cipher, err = encConn.HandshakeOutgoing(sKey, provide, out.Bytes())
		if err != nil {
			log.Debugln("Encrytpion handshake has failed: ", err)
			if forceEncryption {
				log.Debug("Will not try again because ougoing encryption is forced.")
				err = ErrNotEncrypted
				return
			}
			// Connect again and try w/o encryption
			log.Debug("Connecting again without encryption...")
			conn, err = net.DialTimeout(addr.Network(), addr.String(), handshakeDeadline)
			if err != nil {
				return
			}
			log.Debug("Connected")
			// Send BT handshake
			if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
				return
			}
			if _, err = conn.Write(out.Bytes()); err != nil {
				return
			}
		} else {
			log.Debugf("Encryption handshake is successfull. Selected cipher: %d", cipher)
			conn = encConn
			if forceEncryption && cipher == mse.PlainText {
				err = ErrNotEncrypted
				return
			}
		}
	} else {
		// Send BT handshake
		if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		if _, err = conn.Write(out.Bytes()); err != nil {
			return
		}
	}

	// Read BT handshake
	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}

	var ihRead protocol.InfoHash
	peerExtensions, ihRead, err = handshake.Read1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = ErrInvalidInfoHash
		return
	}

	peerID, err = handshake.Read2(conn)
	if err != nil {
		return
	}
	if peerID == ourID {
		err = ErrOwnConnection
		return
	}

	err = conn.SetDeadline(time.Time{})
	return
}

func Accept(
	conn net.Conn, forceEncryption bool,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	hasInfoHash func(protocol.InfoHash) bool,
	ourExtensions [8]byte, ourID protocol.PeerID) (
	cipher mse.CryptoMethod, peerExtensions [8]byte, ih protocol.InfoHash, peerID protocol.PeerID, err error) {

	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}

	encrypted := false
	hasIncomingPayload := false
	var buf bytes.Buffer
	var reader io.Reader = io.TeeReader(conn, &buf)
	peerExtensions, ih, err = handshake.Read1(reader)
	conn = &rwConn{readWriter{io.MultiReader(&buf, conn), conn}, conn}
	if err == handshake.ErrInvalidProtocol {
		encConn := mse.WrapConn(conn)
		payloadIn := make([]byte, 68)
		var lenPayloadIn uint16
		err = encConn.HandshakeIncoming(
			getSKey,
			func(provided mse.CryptoMethod) (selected mse.CryptoMethod) {
				if provided&mse.RC4 != 0 {
					selected = mse.RC4
					encrypted = true
				} else if (provided&mse.PlainText != 0) && !forceEncryption {
					selected = mse.PlainText
				}
				cipher = selected
				return
			},
			payloadIn,
			&lenPayloadIn,
			func() (payloadOut []byte, err error) {
				if lenPayloadIn < 68 {
					// We won't send outgoing initial payload because
					// other side did not send initial payload.
					// We will continue and do encryption negotiation but
					// will do BT handshake after encryption negotiation.
					return nil, nil
				}
				hasIncomingPayload = true
				r := bytes.NewReader(payloadIn[:lenPayloadIn])
				peerExtensions, ih, err = handshake.Read1(r)
				if err != nil {
					return nil, err
				}
				if !hasInfoHash(ih) {
					return nil, ErrInvalidInfoHash
				}
				peerID, err = handshake.Read2(r)
				if err != nil {
					return nil, err
				}
				out := bytes.NewBuffer(make([]byte, 0, 68))
				handshake.Write(out, ih, ourID, ourExtensions)
				return out.Bytes(), nil
			})
		if err == nil {
			conn = encConn
		}
	}
	if err != nil {
		return
	}

	if forceEncryption && !encrypted {
		err = ErrNotEncrypted
		return
	}

	if !hasIncomingPayload {
		if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		peerExtensions, ih, err = handshake.Read1(conn)
		if err != nil {
			return
		}
		if !hasInfoHash(ih) {
			err = ErrInvalidInfoHash
			return
		}
		if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		err = handshake.Write(conn, ih, ourID, ourExtensions)
		if err != nil {
			return
		}
		peerID, err = handshake.Read2(conn)
		if err != nil {
			return
		}
	}

	if peerID == ourID {
		err = ErrOwnConnection
		return
	}

	err = conn.SetDeadline(time.Time{})
	return
}

type readWriter struct {
	io.Reader
	io.Writer
}

type rwConn struct {
	rw io.ReadWriter
	net.Conn
}

func (c *rwConn) Read(p []byte) (n int, err error)  { return c.rw.Read(p) }
func (c *rwConn) Write(p []byte) (n int, err error) { return c.rw.Write(p) }
