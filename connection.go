// Provides support for dialing and accepting BitTorrent connections.

package rain

import (
	"bytes"
	"errors"
	"io"
	"net"
	"time"

	"github.com/cenkalti/mse"
)

const handshakeDeadline = 30 * time.Second

var (
	ErrInvalidInfoHash = errors.New("invalid info hash")
	ErrOwnConnection   = errors.New("dropped own connection")
	ErrNotEncrypted    = errors.New("connection is not encrypted")
)

func Dial(addr net.Addr, enableEncryption, forceEncryption bool, ourExtensions [8]byte, ih InfoHash, ourID PeerID) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID PeerID, err error) {

	log := NewLogger("conn -> " + addr.String())

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
	err = WriteHandshake(out, ih, ourID, ourExtensions)
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

	var ihRead InfoHash
	peerExtensions, ihRead, err = ReadHandshake1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = ErrInvalidInfoHash
		return
	}

	peerID, err = ReadHandshake2(conn)
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
	conn net.Conn,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	hasInfoHash func(InfoHash) bool,
	ourExtensions [8]byte, ourID PeerID) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, ih InfoHash, peerID PeerID, err error) {

	log := NewLogger("conn <- " + conn.RemoteAddr().String())

	if forceEncryption && getSKey == nil {
		panic("forceEncryption && getSKey == nil")
	}

	isEncrypted := false
	hasIncomingPayload := false

	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}

	// Try to do unencrypted handshake first.
	// If protocol string is not valid, try to do encrypted handshake.
	// rwConn returns the read bytes again that is read by handshake.Read1.
	var buf bytes.Buffer
	var reader io.Reader = io.TeeReader(conn, &buf)
	peerExtensions, ih, err = ReadHandshake1(reader)
	conn = &rwConn{readWriter{io.MultiReader(&buf, conn), conn}, conn}

	if err == ErrInvalidProtocol && getSKey != nil {
		mseConn := mse.WrapConn(conn)
		payloadIn := make([]byte, 68)
		var lenPayloadIn uint16
		err = mseConn.HandshakeIncoming(
			getSKey,
			func(provided mse.CryptoMethod) (selected mse.CryptoMethod) {
				if provided&mse.RC4 != 0 {
					selected = mse.RC4
					isEncrypted = true
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
				peerExtensions, ih, err = ReadHandshake1(r)
				if err != nil {
					return nil, err
				}
				if !hasInfoHash(ih) {
					return nil, ErrInvalidInfoHash
				}
				peerID, err = ReadHandshake2(r)
				if err != nil {
					return nil, err
				}
				out := bytes.NewBuffer(make([]byte, 0, 68))
				WriteHandshake(out, ih, ourID, ourExtensions)
				return out.Bytes(), nil
			})
		if err == nil {
			log.Debugf("Encryption handshake is successfull. Selected cipher: %d", cipher)
			conn = mseConn
		}
	}
	if err != nil {
		return
	}

	if forceEncryption && !isEncrypted {
		err = ErrNotEncrypted
		return
	}

	if !hasIncomingPayload {
		if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		peerExtensions, ih, err = ReadHandshake1(conn)
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
		err = WriteHandshake(conn, ih, ourID, ourExtensions)
		if err != nil {
			return
		}
		peerID, err = ReadHandshake2(conn)
		if err != nil {
			return
		}
	}

	if peerID == ourID {
		err = ErrOwnConnection
		return
	}

	err = conn.SetDeadline(time.Time{})
	encConn = conn
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
