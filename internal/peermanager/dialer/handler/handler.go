package handler

import (
	"net"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager/peerids"
)

type Handler struct {
	addr     net.Addr
	peerIDs  *peerids.PeerIDs
	bitfield *bitfield.Bitfield
	peerID   [20]byte
	infoHash [20]byte
	newPeers chan *peer.Peer
	log      logger.Logger
}

func New(addr net.Addr, peerIDs *peerids.PeerIDs, peerID, infoHash [20]byte, newPeers chan *peer.Peer, l logger.Logger) *Handler {
	return &Handler{
		addr:     addr,
		peerIDs:  peerIDs,
		peerID:   peerID,
		infoHash: infoHash,
		newPeers: newPeers,
		log:      l,
	}
}

func (h *Handler) Run(stopC chan struct{}) {
	log := logger.New("peer -> " + h.addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	var ourExtensions [8]byte
	ourbf := bitfield.NewBytes(ourExtensions[:], 64)
	ourbf.Set(61) // Fast Extension

	// TODO separate dial and handshake
	conn, cipher, peerExtensions, peerID, err := btconn.Dial(h.addr, !encryptionDisableOutgoing, encryptionForceOutgoing, ourExtensions, h.infoHash, h.peerID)
	if err != nil {
		log.Errorln("cannot complete handshake:", err)
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, peerID[:8])

	ok := h.peerIDs.Add(peerID)
	if !ok {
		_ = conn.Close()
		return
	}
	defer h.peerIDs.Remove(peerID)

	peerbf := bitfield.NewBytes(peerExtensions[:], 64)
	extensions := ourbf.And(peerbf)

	p := peer.New(conn, peerID, extensions, log)
	select {
	case h.newPeers <- p:
		p.Run(stopC)
	case <-stopC:
	}
}
