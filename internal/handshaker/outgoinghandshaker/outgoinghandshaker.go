package outgoinghandshaker

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peersource"
)

// OutgoingHandshaker does the BitTorrent handshake on an outgoing connection.
type OutgoingHandshaker struct {
	Addr       *net.TCPAddr
	Source     peersource.Source
	Conn       net.Conn
	PeerID     [20]byte
	Extensions [8]byte
	Cipher     mse.CryptoMethod
	Error      error

	closeC chan struct{}
	doneC  chan struct{}
}

// New returns a new OutgoingHandshaker for a TCP address.
func New(addr *net.TCPAddr, source peersource.Source) *OutgoingHandshaker {
	return &OutgoingHandshaker{
		Addr:   addr,
		Source: source,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the handshaker.
func (h *OutgoingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

// Run the handshaker.
func (h *OutgoingHandshaker) Run(dialTimeout, handshakeTimeout time.Duration, peerID, infoHash [20]byte, resultC chan *OutgoingHandshaker, ourExtensions [8]byte, disableOutgoingEncryption, forceOutgoingEncryption bool) {
	defer close(h.doneC)
	log := logger.New("peer -> " + h.Addr.String())

	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.Addr, dialTimeout, handshakeTimeout, !disableOutgoingEncryption, forceOutgoingEncryption, ourExtensions, infoHash, peerID, h.closeC)
	if err != nil {
		if err == io.EOF {
			log.Debug("peer has closed the connection: EOF")
		} else if err == io.ErrUnexpectedEOF {
			log.Debug("peer has closed the connection: Unexpected EOF")
		} else if _, ok := err.(*net.OpError); ok {
			log.Debugln("net operation error:", err)
		} else if _, ok := err.(*btconn.HandshakeError); ok {
			log.Debugln("protocol error:", err)
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

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = peerExtensions
	h.Cipher = cipher

	select {
	case resultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}
