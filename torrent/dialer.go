package torrent

import (
	"net"
	"time"

	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/peer"
)

const (
	noPeerRetryDuration = time.Second
)

func (t *Torrent) dialer() {
	defer t.stopWG.Done()
	for {
		select {
		case t.peerLimiter <- struct{}{}:
			addr := t.nextPeerAddr()
			if addr == nil {
				<-t.peerLimiter
				t.log.Debugln("no more peer to connect, will try again after", noPeerRetryDuration)
				select {
				case <-time.After(noPeerRetryDuration):
				case <-t.stopC:
					return
				}
				continue
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

	p := peer.New(conn, peerID, t.bitfield, log)

	t.m.Lock()
	t.peers[peerID] = p
	t.m.Unlock()
	defer func() {
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	p.Run()
}
