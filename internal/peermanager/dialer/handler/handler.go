package handler

import (
	"net"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

type Handler struct {
	addr        net.Addr
	bitfield    *bitfield.Bitfield
	peerID      [20]byte
	infoHash    [20]byte
	newPeers    chan *peer.Peer
	connectC    chan net.Conn
	disconnectC chan net.Conn
	log         logger.Logger
}

func New(addr net.Addr, peerID, infoHash [20]byte, newPeers chan *peer.Peer, connectC, disconnectC chan net.Conn, l logger.Logger) *Handler {
	return &Handler{
		addr:        addr,
		peerID:      peerID,
		infoHash:    infoHash,
		newPeers:    newPeers,
		connectC:    connectC,
		disconnectC: disconnectC,
		log:         l,
	}
}

func (h *Handler) Run(stopC chan struct{}) {
	log := logger.New("peer -> " + h.addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	// TODO get supported extensions from common place
	var ourExtensions [8]byte
	ourbf := bitfield.NewBytes(ourExtensions[:], 64)
	ourbf.Set(61) // Fast Extension (BEP 6)
	ourbf.Set(43) // Extension Protocol (BEP 10)

	// TODO separate dial and handshake
	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.addr, !encryptionDisableOutgoing, encryptionForceOutgoing, ourExtensions, h.infoHash, h.peerID, stopC, h.connectC, h.disconnectC)
	if err != nil {
		select {
		case <-stopC:
		default:
			log.Errorln("cannot complete handshake:", err)
		}
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	defer func() {
		conn.Close()
		select {
		case h.disconnectC <- conn:
		case <-stopC:
		}
	}()

	peerbf := bitfield.NewBytes(peerExtensions[:], 64)
	extensions := ourbf.And(peerbf)

	p := peer.New(conn, peerID, extensions, log)
	select {
	case h.newPeers <- p:
		p.Run(stopC)
	case <-stopC:
	}
}
