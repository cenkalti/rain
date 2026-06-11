package btconn

import (
	"errors"
	"io"
	"net"

	"github.com/cenkalti/rain/v2/internal/logger"
)

var (
	errInvalidInfoHash = &HandshakeError{"invalid info hash"}
	errOwnConnection   = &HandshakeError{"dropped own connection"}
	errNotEncrypted    = &HandshakeError{"connection is not encrypted"}
	errInvalidProtocol = &HandshakeError{"invalid protocol"}
)

// HandshakeError is an error while doing the protocol handshake.
type HandshakeError struct {
	message string
}

func (e *HandshakeError) Error() string {
	return e.message
}

// LogHandshakeError logs the common, expected handshake failure modes (peer
// closing the connection, network and protocol errors) at debug level and
// reports whether it handled err. When it returns false the failure is
// unexpected and the caller should log err itself, at whatever level fits.
func LogHandshakeError(log logger.Logger, err error) bool {
	var opErr *net.OpError
	var hsErr *HandshakeError
	switch {
	case errors.Is(err, io.EOF):
		log.Debug("peer has closed the connection: EOF")
	case errors.Is(err, io.ErrUnexpectedEOF):
		log.Debug("peer has closed the connection: Unexpected EOF")
	case errors.As(err, &opErr):
		log.Debugln("net operation error:", err)
	case errors.As(err, &hsErr):
		log.Debugln("protocol error:", err)
	default:
		return false
	}
	return true
}
