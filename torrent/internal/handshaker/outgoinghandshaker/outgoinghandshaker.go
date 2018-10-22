package outgoinghandshaker

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
)

type OutgoingHandshaker struct {
	Addr       *net.TCPAddr
	Conn       net.Conn
	PeerID     [20]byte
	Extensions *bitfield.Bitfield
	Error      error

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

func (h *OutgoingHandshaker) Run(dialTimeout, handshakeTimeout time.Duration, peerID, infoHash [20]byte, resultC chan *OutgoingHandshaker, ourExtensions *bitfield.Bitfield, disableOutgoingEncryption, forceOutgoingEncryption bool) {
	defer close(h.doneC)
	log := logger.New("peer -> " + h.Addr.String())

	var ourExtensionsBytes [8]byte
	copy(ourExtensionsBytes[:], ourExtensions.Bytes())

	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.Addr, dialTimeout, handshakeTimeout, !disableOutgoingEncryption, forceOutgoingEncryption, ourExtensionsBytes, infoHash, peerID, h.closeC)
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
	peerbf.And(ourExtensions)

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = peerbf

	select {
	case resultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}
