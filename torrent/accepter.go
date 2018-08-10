package torrent

import (
	"net"

	"github.com/cenkalti/rain/btconn"
	"github.com/cenkalti/rain/logger"
)

func (t *Torrent) accepter() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			t.log.Error(err)
			return
		}
		go t.handleConn(conn)
	}
}

func (t *Torrent) handleConn(conn net.Conn) {
	log := logger.New("peer <- " + conn.RemoteAddr().String())
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Error("cannot close conn:", err)
		}
	}()
	select {
	case t.peerLimiter <- struct{}{}:
		defer func() { <-t.peerLimiter }()
	default:
		log.Debugln("peer limit reached, rejecting peer")
		return
	}

	// TODO get this from config
	encryptionForceIncoming := false

	encConn, cipher, extensions, peerID, _, err := btconn.Accept(
		conn,
		func(sKeyHash [20]byte) (sKey []byte) {
			if sKeyHash == t.sKeyHash {
				return t.metainfo.Info.Hash[:]
			}
			return nil
		},
		encryptionForceIncoming,
		func(infoHash [20]byte) bool {
			return infoHash == t.metainfo.Info.Hash
		},
		[8]byte{}, // no extension for now
		t.peerID,
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

	p := t.newPeer(encConn, peerID, log)

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
