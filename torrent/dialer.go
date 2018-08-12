package torrent

import (
	"net"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

func (t *Torrent) dialer(a *announcer.Announcer) {
	defer t.stopWG.Done()
	for {
		select {
		case t.peerLimiter <- struct{}{}:
			addr := a.NextPeerAddr()
			if addr == nil {
				<-t.peerLimiter
				return
			}
			t.stopWG.Add(1)
			go t.dialAndRun(addr)
		case <-t.stopC:
			return
		}
	}
}

func (t *Torrent) dialAndRun(addr net.Addr) {
	log := logger.New("peer -> " + addr.String())
	defer t.stopWG.Done()
	defer func() { <-t.peerLimiter }()

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	conn, cipher, extensions, peerID, err := btconn.Dial(addr, !encryptionDisableOutgoing, encryptionForceOutgoing, [8]byte{}, t.metainfo.Info.Hash, t.peerID)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			log.Debug(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])
	defer closeConn(conn, log)

	p := peer.New(conn, peerID, t.metainfo.Info.NumPieces, log, t.peerMessages)

	t.m.Lock()
	if _, ok := t.peers[peerID]; ok {
		t.log.Warning("peer already connected, dropping connection")
		return
	}
	t.peers[peerID] = p
	t.m.Unlock()
	defer func() {
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	p.Run(t.bitfield)
}
