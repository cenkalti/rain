package mse

import (
	"net"
	"sync"
)

// Conn is a wrapper around net.Conn that does encryption/decryption on Read/Write methods.
type Conn struct {
	net.Conn
	*Stream
	mr sync.Mutex
	mw sync.Mutex
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
	c.mr.Lock()
	n, err = c.Stream.Read(p)
	c.mr.Unlock()
	return
}

func (c *Conn) Write(p []byte) (n int, err error) {
	c.mw.Lock()
	n, err = c.Stream.Write(p)
	c.mw.Unlock()
	return
}
