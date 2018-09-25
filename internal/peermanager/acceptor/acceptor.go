package acceptor

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager/acceptor/handler"
	"github.com/cenkalti/rain/internal/peermanager/peerids"
	"github.com/cenkalti/rain/internal/worker"
)

const maxAccept = 40

type Acceptor struct {
	port        int
	peerIDs     *peerids.PeerIDs
	peerID      [20]byte
	sKeyHash    [20]byte
	infoHash    [20]byte
	newPeers    chan *peer.Peer
	connectC    chan net.Conn
	disconnectC chan net.Conn
	workers     worker.Workers
	limiter     chan struct{}
	log         logger.Logger
}

func New(port int, peerIDs *peerids.PeerIDs, peerID, infoHash [20]byte, newPeers chan *peer.Peer, connectC, disconnectC chan net.Conn, l logger.Logger) *Acceptor {
	return &Acceptor{
		port:        port,
		peerIDs:     peerIDs,
		peerID:      peerID,
		sKeyHash:    mse.HashSKey(infoHash[:]),
		infoHash:    infoHash,
		newPeers:    newPeers,
		connectC:    connectC,
		disconnectC: disconnectC,
		limiter:     make(chan struct{}, maxAccept),
		log:         l,
	}
}

func (a *Acceptor) Run(stopC chan struct{}) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: a.port})
	if err != nil {
		a.log.Errorf("cannot listen port %d: %s", a.port, err)
		return
	}
	a.log.Notice("Listening peers on tcp://" + listener.Addr().String())

	go func() {
		<-stopC
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-stopC:
				a.workers.Stop()
				return
			default:
			}
			a.log.Error(err)
			return
		}
		select {
		case a.connectC <- conn:
		case <-stopC:
			a.workers.Stop()
			return
		}
		select {
		case a.limiter <- struct{}{}:
			h := handler.New(conn, a.peerIDs, a.peerID, a.sKeyHash, a.infoHash, a.newPeers, a.disconnectC, a.log)
			a.workers.StartWithOnFinishHandler(h, func() { <-a.limiter })
		case <-stopC:
			a.workers.Stop()
			return
		default:
			a.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
			_ = conn.Close()
		}
	}
}
