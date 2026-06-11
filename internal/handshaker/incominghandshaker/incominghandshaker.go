package incominghandshaker

import (
	"net"
	"time"

	"github.com/cenkalti/rain/v2/internal/btconn"
	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/mse"
)

// IncomingHandshaker does the BitTorrent protocol handshake on an incoming connection.
type IncomingHandshaker struct {
	Conn       net.Conn
	PeerID     [20]byte
	Extensions [8]byte
	Cipher     mse.CryptoMethod
	Error      error

	closeC chan struct{}
	doneC  chan struct{}
}

// New returns a new IncomingHandshaker for a net.Conn.
func New(conn net.Conn) *IncomingHandshaker {
	return &IncomingHandshaker{
		Conn:   conn,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the IncomingHandshaker. Also closes the underlying connection if there is an ongoing handshake operation.
func (h *IncomingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

// Run the handshaker goroutine.
func (h *IncomingHandshaker) Run(peerID [20]byte, getSKeyFunc func([20]byte) []byte, checkInfoHashFunc func([20]byte) bool, resultC chan *IncomingHandshaker, timeout time.Duration, ourExtensions [8]byte, forceIncomingEncryption bool) {
	defer close(h.doneC)
	defer func() {
		select {
		case resultC <- h:
		case <-h.closeC:
			h.Conn.Close()
		}
	}()

	log := logger.New("conn <- " + h.Conn.RemoteAddr().String())

	conn, cipher, peerExtensions, peerID, _, err := btconn.Accept(
		h.Conn, timeout, getSKeyFunc, forceIncomingEncryption, checkInfoHashFunc, ourExtensions, peerID)
	if err != nil {
		if !btconn.LogHandshakeError(log, err) {
			log.Debugln("cannot complete incoming handshake:", err)
		}
		h.Error = err
		return
	}
	log.Debugf("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = peerExtensions
	h.Cipher = cipher
}
