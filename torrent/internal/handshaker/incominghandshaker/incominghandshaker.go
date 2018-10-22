package incominghandshaker

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
)

type IncomingHandshaker struct {
	Conn       net.Conn
	PeerID     [20]byte
	Extensions *bitfield.Bitfield
	Error      error

	closeC chan struct{}
	doneC  chan struct{}
}

func New(conn net.Conn) *IncomingHandshaker {
	return &IncomingHandshaker{
		Conn:   conn,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (h *IncomingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

func (h *IncomingHandshaker) Run(peerID [20]byte, getSKeyFunc func([20]byte) []byte, checkInfoHashFunc func([20]byte) bool, resultC chan *IncomingHandshaker, timeout time.Duration, ourExtensions *bitfield.Bitfield, forceIncomingEncryption bool) {
	defer close(h.doneC)
	defer func() {
		select {
		case resultC <- h:
		case <-h.closeC:
			h.Conn.Close()
		}
	}()

	log := logger.New("conn <- " + h.Conn.RemoteAddr().String())

	var ourExtensionsBytes [8]byte
	copy(ourExtensionsBytes[:], ourExtensions.Bytes())

	conn, cipher, peerExtensions, peerID, _, err := btconn.Accept(
		h.Conn, timeout, getSKeyFunc, forceIncomingEncryption, checkInfoHashFunc, ourExtensionsBytes, peerID)
	if err != nil {
		if err == io.EOF {
			log.Debug("peer has closed the connection: EOF")
		} else if err == io.ErrUnexpectedEOF {
			log.Debug("peer has closed the connection: Unexpected EOF")
		} else if _, ok := err.(*net.OpError); ok {
			log.Debugln("net operation error:", err)
		} else {
			log.Errorln("cannot complete incoming handshake:", err)
		}
		h.Error = err
		return
	}
	log.Debugf("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	peerbf := bitfield.NewBytes(peerExtensions[:], 64)
	peerbf.And(ourExtensions)

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = peerbf
}
