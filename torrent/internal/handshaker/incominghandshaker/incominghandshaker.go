package incominghandshaker

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
)

type IncomingHandshaker struct {
	Conn  net.Conn
	Peer  *peerconn.Conn
	Error error

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

func (h *IncomingHandshaker) Run(peerID [20]byte, getSKeyFunc func([20]byte) []byte, checkInfoHashFunc func([20]byte) bool, resultC chan *IncomingHandshaker, l logger.Logger, timeout, readTimeout time.Duration, uploadedBytesCounterC chan int64) {
	defer close(h.doneC)
	defer func() {
		select {
		case resultC <- h:
		case <-h.closeC:
			h.Conn.Close()
		}
	}()

	log := logger.New("peer <- " + h.Conn.RemoteAddr().String())

	// TODO get this from config
	encryptionForceIncoming := false

	// TODO get supported extensions from common place
	ourExtensions := [8]byte{}
	ourbf := bitfield.NewBytes(ourExtensions[:], 64)
	ourbf.Set(61) // Fast Extension (BEP 6)
	ourbf.Set(43) // Extension Protocol (BEP 10)

	conn, cipher, peerExtensions, peerID, _, err := btconn.Accept(
		h.Conn, timeout, getSKeyFunc, encryptionForceIncoming, checkInfoHashFunc, ourExtensions, peerID)
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
	extensions := ourbf.And(peerbf)

	h.Peer = peerconn.New(conn, peerID, extensions, log, readTimeout, uploadedBytesCounterC)
}
