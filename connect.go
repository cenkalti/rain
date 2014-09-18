package rain

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
	errOwnConnection                = errors.New("own connection")
	errInvalidInfoHash              = errors.New("invalid info hash")
	errConnectionNotEncrytpted      = errors.New("connection is not encrypted")
	errPeerDoesNotSupportEncryption = errors.New("peer does not support encryption")
)

func connect(addr *net.TCPAddr, ourExtensions [8]byte, ih protocol.InfoHash, ourID protocol.PeerID) (
	conn net.Conn, peerExtensions [8]byte, peerID protocol.PeerID, err error) {

	log := logger.New("peer -> " + addr.String())

	log.Debug("Connecting to peer...")
	conn, err = net.DialTCP("tcp4", nil, addr)
	if err != nil {
		return
	}
	log.Debug("Connected")
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}
	err = handshake.Write(conn, ih, ourID, ourExtensions)
	if err != nil {
		return
	}

	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}
	var ihRead protocol.InfoHash
	peerExtensions, ihRead, err = handshake.Read1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = errInvalidInfoHash
		return
	}

	peerID, err = handshake.Read2(conn)
	if err != nil {
		return
	}
	if peerID == ourID {
		err = errOwnConnection
		return
	}

	err = conn.SetDeadline(time.Time{})
	return
}

func connectEncrypted(addr *net.TCPAddr, ourExtensions [8]byte, ih protocol.InfoHash, ourID protocol.PeerID, force bool) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID protocol.PeerID, err error) {

	log := logger.New("peer -> " + addr.String())

	// First connection
	log.Debug("Connecting to peer...")
	conn, err = net.DialTCP("tcp4", nil, addr)
	if err != nil {
		return
	}
	log.Debug("Connected")
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	sKey := make([]byte, 20)
	copy(sKey, ih[:])

	provide := mse.RC4
	if !force {
		provide |= mse.PlainText
	}

	out := bytes.NewBuffer(make([]byte, 0, 68))
	handshake.Write(out, ih, ourID, ourExtensions)

	// Try encryption handshake
	encConn := mse.WrapConn(conn)
	cipher, err = encConn.HandshakeOutgoing(sKey, provide, out.Bytes())
	if err != nil {
		log.Debugln("Encrytpion handshake has failed: ", err)
		if force {
			log.Debug("Will not try again because ougoing encryption is forced.")
			err = errPeerDoesNotSupportEncryption
			return
		}
		// Connect again and try w/o encryption
		conn, peerExtensions, peerID, err = connect(addr, ourExtensions, ih, ourID)
		return conn, 0, peerExtensions, peerID, err
	}
	log.Debugf("Encryption handshake is successfull. Selected cipher: %d", cipher)
	conn = encConn

	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}
	var ihRead protocol.InfoHash
	peerExtensions, ihRead, err = handshake.Read1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = errInvalidInfoHash
		return
	}

	peerID, err = handshake.Read2(conn)
	if err != nil {
		return
	}
	if peerID == ourID {
		err = errOwnConnection
		return
	}

	err = conn.SetDeadline(time.Time{})
	return
}

func handshakeIncoming(
	conn net.Conn, forceEncryption bool, ourExtensions [8]byte, id protocol.PeerID,
	getSKey func(sKeyHash [20]byte) (sKey []byte)) (
	cipher mse.CryptoMethod, peerExtensions [8]byte, peerID protocol.PeerID, err error) {

	var ourInfoHash protocol.InfoHash
	getAndSaveInfoHash := func(sKeyHash [20]byte) []byte {
		b := getSKey(sKeyHash)
		copy(ourInfoHash[:], b)
		return b
	}

	if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
		return
	}

	encrypted := false
	hasIncomingPayload := false
	var buf bytes.Buffer
	var reader io.Reader = io.TeeReader(conn, &buf)
	var peerInfoHash protocol.InfoHash
	peerExtensions, peerInfoHash, err = handshake.Read1(reader)
	if err == handshake.ErrInvalidProtocol {
		reader = io.MultiReader(&buf, conn)
		rw := readWriter{reader, conn}
		conn2 := &rwConn{rw, conn}
		encConn := mse.WrapConn(conn2)
		payloadIn := make([]byte, 68)
		var lenPayloadIn uint16
		err = encConn.HandshakeIncoming(
			getAndSaveInfoHash,
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
				peerExtensions, peerInfoHash, err = handshake.Read1(r)
				if err != nil {
					return nil, err
				}
				if peerInfoHash != ourInfoHash {
					return nil, errInvalidInfoHash
				}
				peerID, err = handshake.Read2(r)
				if err != nil {
					return nil, err
				}
				out := bytes.NewBuffer(make([]byte, 0, 68))
				handshake.Write(out, ourInfoHash, id, ourExtensions)
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
		err = errConnectionNotEncrytpted
		return
	}

	if !hasIncomingPayload {
		if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		peerExtensions, peerInfoHash, err = handshake.Read1(conn)
		if err != nil {
			return
		}
		if peerInfoHash != ourInfoHash {
			err = errInvalidInfoHash
			return
		}
		if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		err = handshake.Write(conn, ourInfoHash, id, ourExtensions)
		if err != nil {
			return
		}
		peerID, err = handshake.Read2(conn)
		if err != nil {
			return
		}
	}

	if id == peerID {
		err = errOwnConnection
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
