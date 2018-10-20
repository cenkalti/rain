package btconn

import (
	"bytes"
	"context"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/mse"
)

func Dial(
	addr net.Addr,
	dialTimeout, handshakeTimeout time.Duration,
	enableEncryption,
	forceEncryption bool,
	ourExtensions [8]byte,
	ih [20]byte,
	ourID [20]byte,
	stopC chan struct{}) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, err error) {

	log := logger.New("conn -> " + addr.String())
	done := make(chan struct{})
	defer close(done)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-stopC:
			cancel()
		case <-done:
		}
	}()

	// First connection
	log.Debug("Connecting to peer...")
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err = dialer.DialContext(ctx, addr.Network(), addr.String())
	if err != nil {
		return
	}
	log.Debug("Connected")
	defer func(conn net.Conn) {
		if err != nil {
			conn.Close()
		}
	}(conn)
	go func(conn net.Conn) {
		select {
		case <-stopC:
			conn.Close()
		case <-done:
		}
	}(conn)

	if err = conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return
	}

	out := bytes.NewBuffer(make([]byte, 0, 68))
	err = writeHandshake(out, ih, ourID, ourExtensions)
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
			select {
			case <-stopC:
				return
			default:
			}
			log.Debugln("Encrytpion handshake has failed: ", err)
			if forceEncryption {
				log.Debug("Will not try again because ougoing encryption is forced.")
				err = errNotEncrypted
				return
			}
			// Close current connection
			conn.Close()
			// Connect again and try w/o encryption
			log.Debug("Connecting again without encryption...")
			conn, err = dialer.DialContext(ctx, addr.Network(), addr.String())
			if err != nil {
				return
			}
			log.Debug("Connected")
			defer func(conn net.Conn) {
				if err != nil {
					conn.Close()
				}
			}(conn)
			go func(conn net.Conn) {
				select {
				case <-stopC:
					conn.Close()
				case <-done:
				}
			}(conn)
			// Send BT handshake
			if err = conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
				return
			}
			if _, err = conn.Write(out.Bytes()); err != nil {
				return
			}
		} else {
			log.Debugf("Encryption handshake is successfull. Selected cipher: %d", cipher)
			conn = encConn
			if forceEncryption && cipher == mse.PlainText {
				err = errNotEncrypted
				return
			}
		}
	} else {
		// Send BT handshake
		if _, err = conn.Write(out.Bytes()); err != nil {
			return
		}
	}

	// Read BT handshake
	var ihRead [20]byte
	peerExtensions, ihRead, err = readHandshake1(conn)
	if err != nil {
		return
	}
	if ihRead != ih {
		err = errInvalidInfoHash
		return
	}

	peerID, err = readHandshake2(conn)
	if err != nil {
		return
	}
	if peerID == ourID {
		err = ErrOwnConnection
		return
	}
	return
}
