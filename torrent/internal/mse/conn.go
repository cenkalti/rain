package mse

import (
	"net"
)

// Conn is a wrapper around net.Conn that does encryption/decryption on Read/Write methods.
type Conn struct {
	net.Conn
	*Stream
}

// WrapConn returns a new wrapper around conn. You must call HandshakeIncoming or
// HandshakeOutgoing methods before using Read/Write methods.
func WrapConn(conn net.Conn) *Conn {
	return &Conn{
		Conn:   conn,
		Stream: NewStream(conn),
	}
}

func (c *Conn) Read(p []byte) (n int, err error) {
	n, err = c.Stream.Read(p)
	return
}

func (c *Conn) Write(p []byte) (n int, err error) {
	n, err = c.Stream.Write(p)
	return
}
