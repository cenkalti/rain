package torrent

import (
	"io"
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/btconn"
)

type incomingHandshaker struct {
	conn    net.Conn
	log     logger.Logger
	torrent *Torrent
	closeC  chan struct{}
	doneC   chan struct{}

	Conn       net.Conn
	PeerID     [20]byte
	Extensions *bitfield.Bitfield
	Error      error
}

func (t *Torrent) newIncomingHandshaker(conn net.Conn) *incomingHandshaker {
	return &incomingHandshaker{
		conn:    conn,
		log:     logger.New("peer <- " + conn.RemoteAddr().String()),
		torrent: t,
		closeC:  make(chan struct{}),
		doneC:   make(chan struct{}),
	}
}

func (h *incomingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

func (h *incomingHandshaker) Run() {
	defer close(h.doneC)

	// TODO get this from config
	encryptionForceIncoming := false

	conn, cipher, peerExtensions, peerID, _, err := btconn.Accept(
		h.conn, h.torrent.getSKey, encryptionForceIncoming, h.torrent.checkInfoHash, ourExtensionsBytes, h.torrent.peerID)
	if err != nil {
		if err == io.EOF {
			h.log.Debug("peer has closed the connection: EOF")
		} else if err == io.ErrUnexpectedEOF {
			h.log.Debug("peer has closed the connection: Unexpected EOF")
		} else if _, ok := err.(*net.OpError); ok {
			h.log.Debugln("net operation error:", err)
		} else {
			h.log.Errorln("cannot complete incoming handshake:", err)
		}
		h.Error = err
		select {
		case h.torrent.incomingHandshakerResultC <- h:
		case <-h.closeC:
		}
		return
	}
	h.log.Debugf("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	h.Conn = conn
	h.PeerID = peerID
	h.Extensions = ourExtensions.And(bitfield.NewBytes(peerExtensions[:], 64))

	select {
	case h.torrent.incomingHandshakerResultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}

func (t *Torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

func (t *Torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}
