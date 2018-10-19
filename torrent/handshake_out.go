package torrent

import (
	"io"
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
)

type outgoingHandshaker struct {
	torrent *Torrent
	addr    *net.TCPAddr
	closeC  chan struct{}
	doneC   chan struct{}
	log     logger.Logger

	Conn       net.Conn
	PeerID     [20]byte
	Extensions *bitfield.Bitfield
	Error      error
}

func (t *Torrent) newOutgoingHandshaker(addr *net.TCPAddr) *outgoingHandshaker {
	return &outgoingHandshaker{
		torrent: t,
		addr:    addr,
		closeC:  make(chan struct{}),
		doneC:   make(chan struct{}),
		log:     logger.New("peer -> " + addr.String()),
	}
}

func (h *outgoingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

func (h *outgoingHandshaker) Run() {
	defer close(h.doneC)

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.addr, !encryptionDisableOutgoing, encryptionForceOutgoing, ourExtensionsBytes, h.torrent.infoHash, h.torrent.peerID, h.closeC)
	if err != nil {
		if err == io.EOF {
			h.log.Debug("peer has closed the connection: EOF")
		} else if err == io.ErrUnexpectedEOF {
			h.log.Debug("peer has closed the connection: Unexpected EOF")
		} else if _, ok := err.(*net.OpError); ok {
			h.log.Debugln("net operation error:", err)
		} else {
			h.log.Errorln("cannot complete outgoing handshake:", err)
		}
		h.Error = err
		select {
		case h.torrent.outgoingHandshakerResultC <- h:
		case <-h.closeC:
		}
		return
	}
	h.log.Debugf("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = ourExtensions.And(bitfield.NewBytes(peerExtensions[:], 64))

	select {
	case h.torrent.outgoingHandshakerResultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}
