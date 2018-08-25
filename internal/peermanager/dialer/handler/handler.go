package handler

import (
	"net"

	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager/peerids"
	"github.com/cenkalti/rain/internal/torrentdata"
)

type Handler struct {
	addr     net.Addr
	peerIDs  *peerids.PeerIDs
	data     *torrentdata.Data
	peerID   [20]byte
	infoHash [20]byte
	messages *peer.Messages
	log      logger.Logger
}

func New(addr net.Addr, peerIDs *peerids.PeerIDs, data *torrentdata.Data, peerID, infoHash [20]byte, messages *peer.Messages, l logger.Logger) *Handler {
	return &Handler{
		addr:     addr,
		peerIDs:  peerIDs,
		data:     data,
		peerID:   peerID,
		infoHash: infoHash,
		messages: messages,
		log:      l,
	}
}

func (h *Handler) Run(stopC chan struct{}) {
	log := logger.New("peer -> " + h.addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	conn, cipher, extensions, peerID, err := btconn.Dial(
		h.addr, !encryptionDisableOutgoing, encryptionForceOutgoing, [8]byte{}, h.infoHash, h.peerID)
	if err != nil {
		log.Error(err)
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	// TODO separate dial and handshake

	ok := h.peerIDs.Add(peerID)
	if !ok {
		_ = conn.Close()
		return
	}
	defer h.peerIDs.Remove(peerID)

	p := peer.New(conn, peerID, h.data, log, h.messages)
	p.Run(stopC)
}
