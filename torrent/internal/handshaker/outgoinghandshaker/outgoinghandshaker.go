package outgoinghandshaker

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
)

type OutgoingHandshaker struct {
	Addr  *net.TCPAddr
	Peer  *peerconn.Conn
	Error error

	closeC chan struct{}
	doneC  chan struct{}
}

func New(addr *net.TCPAddr) *OutgoingHandshaker {
	return &OutgoingHandshaker{
		Addr:   addr,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (h *OutgoingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

func (h *OutgoingHandshaker) Run(dialTimeout, handshakeTimeout, readTimeout time.Duration, peerID, infoHash [20]byte, resultC chan *OutgoingHandshaker, l logger.Logger, uploadedBytesCounterC chan int64) {
	defer close(h.doneC)
	log := logger.New("peer -> " + h.Addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	// TODO get supported extensions from common place
	var ourExtensions [8]byte
	ourbf := bitfield.NewBytes(ourExtensions[:], 64)
	ourbf.Set(61) // Fast Extension (BEP 6)
	ourbf.Set(43) // Extension Protocol (BEP 10)

	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.Addr, dialTimeout, handshakeTimeout, !encryptionDisableOutgoing, encryptionForceOutgoing, ourExtensions, infoHash, peerID, h.closeC)
	if err != nil {
		if err == io.EOF {
			log.Debug("peer has closed the connection: EOF")
		} else if err == io.ErrUnexpectedEOF {
			log.Debug("peer has closed the connection: Unexpected EOF")
		} else if _, ok := err.(*net.OpError); ok {
			log.Debugln("net operation error:", err)
		} else {
			log.Errorln("cannot complete outgoing handshake:", err)
		}
		h.Error = err
		select {
		case resultC <- h:
		case <-h.closeC:
		}
		return
	}
	log.Debugf("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	peerbf := bitfield.NewBytes(peerExtensions[:], 64)
	peerbf.And(ourbf)

	h.Peer = peerconn.New(conn, peerID, peerbf, log, readTimeout, uploadedBytesCounterC)
	select {
	case resultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}
