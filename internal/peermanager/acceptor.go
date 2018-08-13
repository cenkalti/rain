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
		go func() {
			defer m.wg.Done()
			m.handleConn(conn)
		}()
	}
}

func (m *PeerManager) handleConn(conn net.Conn) {
	log := logger.New("peer <- " + conn.RemoteAddr().String())
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Errorln("cannot close conn:", err)
		}
	}()

	select {
	case m.limiter <- struct{}{}:
		defer func() { <-m.limiter }()
	default:
		log.Debugln("peer limit reached, rejecting peer")
		return
	}

	// TODO get this from config
	encryptionForceIncoming := false

	encConn, cipher, extensions, peerID, _, err := btconn.Accept(
		conn,
		func(sKeyHash [20]byte) (sKey []byte) {
			if sKeyHash == m.sKeyHash {
				return m.infoHash[:]
			}
			return nil
		},
		encryptionForceIncoming,
		func(infoHash [20]byte) bool {
			return infoHash == m.infoHash
		},
		[8]byte{}, // no extension for now
		m.peerID,
	)
	if err != nil {
		if err == btconn.ErrOwnConnection {
			log.Warning(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])

	p := peer.New(encConn, peerID, m.bitfield.Len(), log, m.peerMessages)
	m.peerConnected <- p
	p.Run(m.bitfield)
}
