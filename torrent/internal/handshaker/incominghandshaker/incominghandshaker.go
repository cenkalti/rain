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
	Peer  *peerconn.Conn
	Conn  net.Conn
	Error error

	peerID                [20]byte
	sKeyHash              [20]byte
	infoHash              [20]byte
	resultC               chan *IncomingHandshaker
	timeout               time.Duration
	readTimeout           time.Duration
	uploadedBytesCounterC chan int64
	closeC                chan struct{}
	doneC                 chan struct{}
	log                   logger.Logger
}

func New(conn net.Conn, peerID, sKeyHash, infoHash [20]byte, resultC chan *IncomingHandshaker, l logger.Logger, timeout, readTimeout time.Duration, uploadedBytesCounterC chan int64) *IncomingHandshaker {
	return &IncomingHandshaker{
		Conn:                  conn,
		peerID:                peerID,
		sKeyHash:              sKeyHash,
		infoHash:              infoHash,
		resultC:               resultC,
		timeout:               timeout,
		readTimeout:           readTimeout,
		uploadedBytesCounterC: uploadedBytesCounterC,
		closeC:                make(chan struct{}),
		doneC:                 make(chan struct{}),
		log:                   l,
	}
}

func (h *IncomingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

func (h *IncomingHandshaker) Run() {
	defer close(h.doneC)
	defer func() {
		select {
		case h.resultC <- h:
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
		h.Conn, h.timeout, h.getSKey, encryptionForceIncoming, h.checkInfoHash, ourExtensions, h.peerID)
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

	h.Peer = peerconn.New(conn, peerID, extensions, log, h.readTimeout, h.uploadedBytesCounterC)
}

func (h *IncomingHandshaker) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == h.sKeyHash {
		return h.infoHash[:]
	}
	return nil
}

func (h *IncomingHandshaker) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == h.infoHash
}
