package peermanager

import (
	"net"

	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

func (m *PeerManager) dialer(stopC chan struct{}) {
	for {
		select {
		case m.limiter <- struct{}{}:
			nextPeer := m.peerList.Get()
			select {
			case addr := <-nextPeer:
				m.wg.Add(1)
				go func() {
					defer m.wg.Done()
					defer func() { <-m.limiter }()
					m.dialAndRun(addr, stopC)
				}()
			case <-stopC:
				return
			}
		case <-stopC:
			return
		}
	}
}

func (m *PeerManager) dialAndRun(addr net.Addr, stopC chan struct{}) {
	log := logger.New("peer -> " + addr.String())

	// TODO get this from config
	encryptionDisableOutgoing := false
	encryptionForceOutgoing := false

	conn, cipher, extensions, peerID, err := btconn.Dial(
		addr, !encryptionDisableOutgoing, encryptionForceOutgoing, [8]byte{}, m.infoHash, m.peerID)
	if err != nil {
		log.Error(err)
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	defer func() {
		err := conn.Close()
		if err != nil {
			log.Errorln("cannot close conn:", err)
		}
	}()

	p := peer.New(conn, peerID, m.data, log, m.peerMessages)
	m.handleConnect(p, stopC)
}
