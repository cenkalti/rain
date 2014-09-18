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
)

type EncryptionMode int

const (
	Enabled EncryptionMode = iota
	Disabled
	Force
)

var (
	errOwnConnection                = errors.New("own connection")
	errInvalidInfoHash              = errors.New("invalid info hash")
	errConnectionNotEncrytpted      = errors.New("connection is not encrypted")
	errPeerDoesNotSupportEncryption = errors.New("peer does not support encryption")
)

func newOutgoingConnection(addr *net.TCPAddr, encMode EncryptionMode, ih protocol.InfoHash, id protocol.PeerID) (
	conn net.Conn, extensions [8]byte, err error) {

	defer func() {
		if err != nil {
			conn.Close()
		}
	}()

	log := logger.New("peer -> " + addr.String())

	// First connection
	log.Debug("Connecting to peer...")
	conn, err = net.DialTCP("tcp4", nil, addr)
	if err != nil {
		return
	}
	log.Debug("Connected")

	// Outgoing handshake bytes
	bt := make([]byte, 0, 68)
	buf := bytes.NewBuffer(bt)
	writeHandShake(buf, ih, id, [8]byte{})
	bt = buf.Bytes()

	sendBThandshake := func() error {
		conn.SetDeadline(time.Now().Add(30 * time.Second))
		_, err = conn.Write(bt)
		return err
	}

	if encMode != Disabled {
		// Try encryption handshake
		encConn := mse.WrapConn(conn)
		sKey := make([]byte, 20)
		copy(sKey, ih[:])
		provide := mse.RC4
		if encMode != Force {
			provide |= mse.PlainText
		}
		var selected mse.CryptoMethod
		selected, err = encConn.HandshakeOutgoing(sKey, provide, bt)
		if err != nil {
			log.Debugln("Encrytpion handshake has failed: ", err)
			if encMode != Force {
				// Connect again and try w/o encryption
				log.Debug("Connecting again for unencrypted handshake...")
				conn, err = net.DialTCP("tcp4", nil, addr)
				if err != nil {
					return
				}
				log.Debug("Connected")

				if err = sendBThandshake(); err != nil {
					return
				}
			} else {
				log.Debug("Will not try again because ougoing encryption is forced.")
				err = errPeerDoesNotSupportEncryption
				return
			}
		} else {
			log.Debugf("Encryption handshake is successfull. Selected cipher: %d", selected)
			// No need to check selected cipher here because it is already checked by encConn.HandshakeOutgoing.
			conn = encConn
		}
	} else {
		if err = sendBThandshake(); err != nil {
			return
		}
	}

	var ihRead protocol.InfoHash
	extensions, ihRead, err = readHandShake1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = errInvalidInfoHash
		return
	}

	var idRead protocol.PeerID
	idRead, err = readHandShake2(conn)
	if err != nil {
		return
	}
	if idRead == id {
		err = errOwnConnection
		return
	}

	return
}

func handshakeIncoming(
	conn net.Conn, encMode EncryptionMode, extensions [8]byte, id protocol.PeerID,
	getSKey func(sKeyHash [20]byte) (sKey []byte)) (
	peerExtensions [8]byte, ih protocol.InfoHash, peerID protocol.PeerID, err error) {

	if encMode == Disabled {
		panic("incoming encryption cannot be disabled") // TODO
	}

	// // Give a minute for completing handshake.
	// err := conn.SetDeadline(time.Now().Add(time.Minute))
	// if err != nil {
	// 	return
	// }

	var ourInfoHash protocol.InfoHash
	getAndSaveInfoHash := func(sKeyHash [20]byte) []byte {
		b := getSKey(sKeyHash)
		copy(ourInfoHash[:], b)
		return b
	}

	encrypted := false
	hasIncomingPayload := false
	var buf bytes.Buffer
	var reader io.Reader = io.TeeReader(conn, &buf)
	peerExtensions, ih, err = readHandShake1(reader)
	if err == errInvalidProtocol {
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
				}
				if encMode != Force && (provided&mse.PlainText != 0) {
					selected = mse.PlainText
				}
				return
			},
			payloadIn,
			&lenPayloadIn,
			func() (payloadOut []byte, err error) {
				if lenPayloadIn != 68 {
					// We won't send outgoing initial payload because other side did not send initial payload.
					// We will do BT handshake after encryption negotiation.
					return nil, nil
				}
				hasIncomingPayload = true
				r := bytes.NewReader(payloadIn[:lenPayloadIn])
				peerExtensions, ih, err = readHandShake1(r)
				if err != nil {
					return nil, err
				}
				if ih != ourInfoHash {
					return nil, errInvalidInfoHash
				}
				peerID, err = readHandShake2(r)
				if err != nil {
					return nil, err
				}
				out := make([]byte, 0, 68)
				err = writeHandShake(bytes.NewBuffer(out), ourInfoHash, id, extensions)
				return out, nil
			})
		if err != nil {
			return
		}

	} else if err != nil {
		return
	} else {
		// Incoming connection is unencrypted.
	}

	if encMode == Force && !encrypted {
		err = errConnectionNotEncrytpted
		return
	}

	if ih != ourInfoHash {
		err = errInvalidInfoHash
		return
	}

	err = writeHandShake(conn, ih, id, extensions)
	if err != nil {
		return
	}

	peerID, err = readHandShake2(conn)
	if err != nil {
		return
	}
	if id == peerID {
		err = errOwnConnection
		return
	}

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
