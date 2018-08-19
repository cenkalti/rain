package peermanager

import (
	"net"

	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

func (m *PeerManager) acceptor(stopC chan struct{}) {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-stopC:
				return
			default:
			}
			m.log.Error(err)
			return
		}
		m.wg.Add(1)
		go m.handleConn(conn, stopC)
	}
}

func (m *PeerManager) handleConn(conn net.Conn, stopC chan struct{}) {
	log := logger.New("peer <- " + conn.RemoteAddr().String())

	select {
	case m.limiter <- struct{}{}:
	default:
		log.Debugln("peer limit reached, rejecting peer")
		return
	}

	// TODO get this from config
	encryptionForceIncoming := false
	extensions := [8]byte{}

	encConn, cipher, extensions, peerID, _, err := btconn.Accept(
		conn, m.getSKey, encryptionForceIncoming, m.checkInfoHash, extensions, m.peerID)
	if err != nil {
		log.Error(err)
		err2 := conn.Close()
		if err2 != nil {
			log.Errorln("cannot close conn:", err2)
		}
		<-m.limiter
		m.wg.Done()
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	p := peer.New(encConn, peerID, m.data, log, m.peerMessages)
	select {
	case m.peerConnected <- p:
	case <-stopC:
		m.wg.Done()
	}
}

func (m *PeerManager) getSKey(sKeyHash [20]byte) []byte {
	if sKeyHash == m.sKeyHash {
		return m.infoHash[:]
	}
	return nil
}

func (m *PeerManager) checkInfoHash(infoHash [20]byte) bool {
	return infoHash == m.infoHash
}
