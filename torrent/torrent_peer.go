package torrent

import (
	"context"
	"net"
	"strconv"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/peersource"
	"github.com/cenkalti/rain/internal/resolver"
)

func (t *torrent) setNeedMorePeers(val bool) {
	for _, an := range t.announcers {
		an.NeedMorePeers(val)
	}
	if t.dhtAnnouncer != nil {
		t.dhtAnnouncer.NeedMorePeers(val)
	}
}

func (t *torrent) addPeerString(addr string) error {
	hoststr, portstr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port64, err := strconv.ParseUint(portstr, 10, 16)
	if err != nil {
		return err
	}
	port := int(port64)
	ip := net.ParseIP(hoststr)
	if ip == nil {
		go t.resolveAndAddPeer(hoststr, port)
		return nil
	}
	taddr := &net.TCPAddr{
		IP:   ip,
		Port: port,
	}
	go t.AddPeers([]*net.TCPAddr{taddr})
	return nil
}

func (t *torrent) resolveAndAddPeer(host string, port int) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		select {
		case <-t.closeC:
		case <-done:
		}
		cancel()
	}()
	ip, err := resolver.ResolveIPv4(ctx, t.session.config.DNSResolveTimeout, host)
	if err != nil {
		return
	}
	addrs := []*net.TCPAddr{{IP: ip, Port: port}}
	t.handleNewPeers(addrs, peersource.Manual)
}

func (t *torrent) handleNewPeers(addrs []*net.TCPAddr, source peersource.Source) {
	t.log.Debugf("received %d peers from %s", len(addrs), source)
	t.setNeedMorePeers(false)
	if status := t.status(); status == Stopped || status == Stopping {
		return
	}
	if !t.completed {
		addrs = t.filterBannedIPs(addrs)
		t.addrList.Push(addrs, source)
		t.dialAddresses()
	}
}

func (t *torrent) filterBannedIPs(a []*net.TCPAddr) []*net.TCPAddr {
	b := a[:0]
	for _, x := range a {
		if _, ok := t.bannedPeerIPs[x.IP.String()]; !ok {
			b = append(b, x)
		}
	}
	return b
}

func (t *torrent) dialAddresses() {
	if t.completed {
		return
	}
	peersConnected := func() int {
		return len(t.outgoingPeers) + len(t.outgoingHandshakers)
	}
	for peersConnected() < t.session.config.MaxPeerDial {
		addr, src := t.addrList.Pop()
		if addr == nil {
			t.setNeedMorePeers(true)
			return
		}
		ip := addr.IP.String()
		if _, ok := t.connectedPeerIPs[ip]; ok {
			continue
		}
		h := outgoinghandshaker.New(addr, src)
		t.outgoingHandshakers[h] = struct{}{}
		t.connectedPeerIPs[ip] = struct{}{}
		go h.Run(
			t.session.config.PeerConnectTimeout,
			t.session.config.PeerHandshakeTimeout,
			t.peerID,
			t.infoHash,
			t.outgoingHandshakerResultC,
			t.session.extensions,
			t.session.config.DisableOutgoingEncryption,
			t.session.config.ForceOutgoingEncryption,
		)
	}
}

func (t *torrent) startPeer(
	conn net.Conn,
	source peersource.Source,
	peers map[*peer.Peer]struct{},
	peerID [20]byte,
	extensions [8]byte,
	cipher mse.CryptoMethod,
) {
	addr := conn.RemoteAddr().(*net.TCPAddr)
	t.pexAddPeer(addr)
	_, ok := t.peerIDs[peerID]
	if ok {
		t.log.Debugf("peer with same id already connected. addr: %s id: %s", addr, peerID)
		conn.Close()
		t.pexDropPeer(addr)
		t.dialAddresses()
		return
	}
	t.peerIDs[peerID] = struct{}{}

	pe := peer.New(conn, source, peerID, extensions, cipher, t.session.config.PieceReadTimeout, t.session.config.RequestTimeout, t.session.config.MaxRequestsIn, t.session.bucketDownload, t.session.bucketUpload)
	t.peers[pe] = struct{}{}
	peers[pe] = struct{}{}
	if t.info != nil {
		pe.Bitfield = bitfield.New(t.info.NumPieces)
	}
	go pe.Run(t.messages, t.pieceMessagesC.SendC(), t.peerSnubbedC, t.peerDisconnectedC)
	t.session.metrics.Peers.Inc(1)
	t.sendFirstMessage(pe)
	t.recentlySeen.Add(pe.Addr())
}

func (t *torrent) sendFirstMessage(p *peer.Peer) {
	bf := t.bitfield
	switch {
	case p.FastEnabled && bf != nil && bf.All():
		msg := peerprotocol.HaveAllMessage{}
		p.SendMessage(msg)
	case p.FastEnabled && (bf == nil || bf.Count() == 0):
		msg := peerprotocol.HaveNoneMessage{}
		p.SendMessage(msg)
	case bf != nil:
		bitfieldData := make([]byte, len(bf.Bytes()))
		copy(bitfieldData, bf.Bytes())
		msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
		p.SendMessage(&msg)
	}
	var metadataSize uint32
	if t.info != nil {
		metadataSize = uint32(len(t.info.Bytes))
	}
	if p.ExtensionsEnabled {
		extHandshakeMsg := peerprotocol.NewExtensionHandshake(metadataSize, t.getClientVersion(), p.Addr().IP, t.session.config.MaxRequestsIn)
		msg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: peerprotocol.ExtensionIDHandshake,
			Payload:           extHandshakeMsg,
		}
		p.SendMessage(msg)
	}
	if p.DHTEnabled {
		msg := peerprotocol.PortMessage{Port: t.session.config.DHTPort}
		p.SendMessage(msg)
	}
	if p.FastEnabled && t.pieces != nil {
		p.GenerateAndSendAllowedFastMessages(t.session.config.AllowedFastSet, t.info.NumPieces, t.infoHash, t.pieces)
	}
}

func (t *torrent) getClientVersion() string {
	if t.info != nil && t.info.Private {
		return t.session.config.PrivateExtensionHandshakeClientVersion
	}
	return publicExtensionHandshakeClientVersion
}

// Process messages received while we don't have metadata yet.
func (t *torrent) processQueuedMessages() {
	for pe := range t.peers {
		for _, msg := range pe.Messages {
			pm := peer.Message{Peer: pe, Message: msg}
			t.handlePeerMessage(pm)
		}
		pe.Messages = nil
	}
}

func (t *torrent) handlePeerSnubbed(pe *peer.Peer) {
	// Mark slow peer as snubbed to skip that peer in piece picker
	if pd, ok := t.pieceDownloaders[pe]; ok {
		// Snub timer is already stopped on choke message but may fire anyway.
		if pe.PeerChoking {
			return
		}
		pe.Snubbed = true
		t.pieceDownloadersSnubbed[pe] = pd
		if t.piecePicker != nil {
			t.piecePicker.HandleSnubbed(pe, pd.Piece.Index)
		}
		t.startPieceDownloaders()
	} else if id, ok := t.infoDownloaders[pe]; ok {
		pe.Snubbed = true
		t.infoDownloadersSnubbed[pe] = id
		t.startInfoDownloaders()
	}
}
