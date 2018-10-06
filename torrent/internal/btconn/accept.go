package btconn

import (
	"bytes"
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/torrent/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/mse"
)

func Accept(
	conn net.Conn,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	hasInfoHash func([20]byte) bool,
	ourExtensions [8]byte, ourID [20]byte) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, infoHash [20]byte, err error) {

	log := logger.New("conn <- " + conn.RemoteAddr().String())

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
	var reader = io.TeeReader(conn, &buf)
	peerExtensions, infoHash, err = readHandshake1(reader)
	conn = &rwConn{readWriter{io.MultiReader(&buf, conn), conn}, conn}

	if err == errInvalidProtocol && getSKey != nil {
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
				peerExtensions, infoHash, err = readHandshake1(r)
				if err != nil {
					return nil, err
				}
				if !hasInfoHash(infoHash) {
					return nil, errInvalidInfoHash
				}
				peerID, err = readHandshake2(r)
				if err != nil {
					return nil, err
				}
				out := bytes.NewBuffer(make([]byte, 0, 68))
				err = writeHandshake(out, infoHash, ourID, ourExtensions)
				return out.Bytes(), err
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
		err = errNotEncrypted
		return
	}

	if !hasIncomingPayload {
		if err = conn.SetReadDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		peerExtensions, infoHash, err = readHandshake1(conn)
		if err != nil {
			return
		}
		if !hasInfoHash(infoHash) {
			err = errInvalidInfoHash
			return
		}
		if err = conn.SetWriteDeadline(time.Now().Add(handshakeDeadline)); err != nil {
			return
		}
		err = writeHandshake(conn, infoHash, ourID, ourExtensions)
		if err != nil {
			return
		}
		peerID, err = readHandshake2(conn)
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
