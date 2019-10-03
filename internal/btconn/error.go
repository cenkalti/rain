package btconn

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
