package torrent

import (
	"net"

	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
)

func (t *torrent) handleNewConnection(conn net.Conn) {
	if len(t.incomingHandshakers)+len(t.incomingPeers) >= t.config.MaxPeerAccept {
		t.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
		conn.Close()
		return
	}
	ip := conn.RemoteAddr().(*net.TCPAddr).IP
	ipstr := ip.String()
	if t.blocklist != nil && t.blocklist.Blocked(ip) {
		t.log.Debugln("peer is blocked:", conn.RemoteAddr().String())
		conn.Close()
		return
	}
	if _, ok := t.connectedPeerIPs[ipstr]; ok {
		t.log.Debugln("received duplicate connection from same IP: ", conn.RemoteAddr().String())
		conn.Close()
		return
	}
	h := incominghandshaker.New(conn)
	t.incomingHandshakers[h] = struct{}{}
	t.connectedPeerIPs[ipstr] = struct{}{}
	go h.Run(
		t.peerID,
		t.getSKey,
		t.checkInfoHash,
		t.incomingHandshakerResultC,
		t.config.PeerHandshakeTimeout,
		ourExtensions,
		t.config.ForceIncomingEncryption,
	)
}
