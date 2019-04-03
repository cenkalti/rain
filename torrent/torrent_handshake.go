package torrent

import (
	"net"

	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/peersource"
)

func (t *torrent) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == t.sKeyHash {
		return t.infoHash[:]
	}
	return nil
}

func (t *torrent) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == t.infoHash
}

func (t *torrent) handleIncomingHandshakeDone(ih *incominghandshaker.IncomingHandshaker) {
	delete(t.incomingHandshakers, ih)
	if ih.Error != nil {
		delete(t.connectedPeerIPs, ih.Conn.RemoteAddr().(*net.TCPAddr).IP.String())
		return
	}
	t.startPeer(ih.Conn, peersource.Incoming, t.incomingPeers, ih.PeerID, ih.Extensions, ih.Cipher)
}

func (t *torrent) handleOutgoingHandshakeDone(oh *outgoinghandshaker.OutgoingHandshaker) {
	delete(t.outgoingHandshakers, oh)
	if oh.Error != nil {
		delete(t.connectedPeerIPs, oh.Addr.IP.String())
		t.dialAddresses()
		return
	}
	t.startPeer(oh.Conn, oh.Source, t.outgoingPeers, oh.PeerID, oh.Extensions, oh.Cipher)
}
