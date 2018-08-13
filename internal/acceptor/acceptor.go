package acceptor

import (
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/btconn"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
)

type Acceptor struct {
	listener      *net.TCPListener
	peerID        [20]byte
	infoHash      [20]byte
	sKeyHash      [20]byte
	bitfield      *bitfield.Bitfield
	limiter       chan struct{}
	peerConnected chan *peer.Peer
	peerMessages  chan peer.Message
	log           logger.Logger
}

func New(listener *net.TCPListener, peerID, infoHash [20]byte, b *bitfield.Bitfield, limiter chan struct{}, peerConnected chan *peer.Peer, peerMessages chan peer.Message, l logger.Logger) *Acceptor {
	return &Acceptor{
		listener:      listener,
		peerID:        peerID,
		infoHash:      infoHash,
		sKeyHash:      mse.HashSKey(infoHash[:]),
		bitfield:      b,
		limiter:       limiter,
		peerConnected: peerConnected,
		peerMessages:  peerMessages,
		log:           l,
	}
}

func (a *Acceptor) Run(stopC chan struct{}) {
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-stopC:
				return
			default:
			}
			a.log.Error(err)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.handleConn(conn)
		}()
	}
}

func (a *Acceptor) handleConn(conn net.Conn) {
	log := logger.New("peer <- " + conn.RemoteAddr().String())
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Errorln("cannot close conn:", err)
		}
	}()

	select {
	case a.limiter <- struct{}{}:
		defer func() { <-a.limiter }()
	default:
		log.Debugln("peer limit reached, rejecting peer")
		return
	}

	// TODO get this from config
	encryptionForceIncoming := false

	encConn, cipher, extensions, peerID, _, err := btconn.Accept(
		conn,
		func(sKeyHash [20]byte) (sKey []byte) {
			if sKeyHash == a.sKeyHash {
				return a.infoHash[:]
			}
			return nil
		},
		encryptionForceIncoming,
		func(infoHash [20]byte) bool {
			return infoHash == a.infoHash
		},
		[8]byte{}, // no extension for now
		a.peerID,
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

	p := peer.New(encConn, peerID, a.bitfield.Len(), log, a.peerMessages)
	a.peerConnected <- p
	p.Run(a.bitfield)
}
