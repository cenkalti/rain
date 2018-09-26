package peermanager

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager/acceptor"
	"github.com/cenkalti/rain/internal/peermanager/dialer"
	"github.com/cenkalti/rain/internal/peermanager/peerids"
	"github.com/cenkalti/rain/internal/worker"
)

type PeerManager struct {
	port        int
	addrToCon   chan *net.TCPAddr
	peerIDs     *peerids.PeerIDs
	peerID      [20]byte
	infoHash    [20]byte
	newPeers    chan *peer.Peer
	conns       map[string]net.Conn
	connectC    chan net.Conn
	disconnectC chan net.Conn
	workers     worker.Workers
	log         logger.Logger
}

func New(port int, addrToCon chan *net.TCPAddr, peerID, infoHash [20]byte, newPeers chan *peer.Peer, l logger.Logger) *PeerManager {
	return &PeerManager{
		port:        port,
		addrToCon:   addrToCon,
		peerIDs:     peerids.New(),
		peerID:      peerID,
		infoHash:    infoHash,
		newPeers:    newPeers,
		conns:       make(map[string]net.Conn),
		connectC:    make(chan net.Conn),
		disconnectC: make(chan net.Conn),
		log:         l,
	}
}

func (m *PeerManager) NewPeers() <-chan *peer.Peer {
	return m.newPeers
}

func (m *PeerManager) Run(stopC chan struct{}) {
	a := acceptor.New(m.port, m.peerIDs, m.peerID, m.infoHash, m.newPeers, m.connectC, m.disconnectC, m.log)
	m.workers.Start(a)

	d := dialer.New(m.addrToCon, m.peerIDs, m.peerID, m.infoHash, m.newPeers, m.connectC, m.disconnectC, m.log)
	m.workers.Start(d)

	for {
		select {
		case conn := <-m.connectC:
			m.conns[conn.RemoteAddr().String()] = conn
		case conn := <-m.disconnectC:
			delete(m.conns, conn.RemoteAddr().String())
		case <-stopC:
			for _, conn := range m.conns {
				conn.Close()
			}
			m.workers.Stop()
			return
		}
	}
}
