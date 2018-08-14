package peermanager

import (
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/torrentdata"
)

// Do not connect more than maxPeerPerTorrent peers.
const maxPeerPerTorrent = 60

type PeerManager struct {
	peers            map[[20]byte]*peer.Peer // connected peers
	peerDisconnected chan *peer.Peer
	port             int
	listener         *net.TCPListener
	announcer        *announcer.Announcer
	peerID           [20]byte
	infoHash         [20]byte
	sKeyHash         [20]byte
	data             *torrentdata.Data
	limiter          chan struct{}
	peerMessages     chan peer.Message
	log              logger.Logger
	m                sync.Mutex
	wg               sync.WaitGroup
}

func New(port int, a *announcer.Announcer, peerID, infoHash [20]byte, d *torrentdata.Data, l logger.Logger) *PeerManager {
	return &PeerManager{
		peers:            make(map[[20]byte]*peer.Peer),
		peerDisconnected: make(chan *peer.Peer),
		port:             port,
		announcer:        a,
		peerID:           peerID,
		infoHash:         infoHash,
		sKeyHash:         mse.HashSKey(infoHash[:]),
		data:             d,
		limiter:          make(chan struct{}, maxPeerPerTorrent),
		peerMessages:     make(chan peer.Message),
		log:              l,
	}
}

func (m *PeerManager) PeerMessages() chan peer.Message {
	return m.peerMessages
}

func (m *PeerManager) close() {
	m.m.Lock()
	defer m.m.Unlock()
	if m.listener != nil {
		err := m.listener.Close()
		if err != nil {
			m.log.Errorln("cannot close listener:", err)
		}
	}
	for _, p := range m.peers {
		err := p.Close()
		if err != nil {
			m.log.Errorln("cannot close peer:", err)
		}
	}
}

func (m *PeerManager) Run(stopC chan struct{}) {
	m.startAcceptor(stopC)
	m.startDialer(stopC)
	for {
		select {
		case p := <-m.peerDisconnected:
			m.handleDisconnect(p)
		case <-stopC:
			m.close()
			m.wg.Done()
			return
		}
	}
}

func (m *PeerManager) startAcceptor(stopC chan struct{}) {
	m.m.Lock()
	defer m.m.Unlock()

	var err error
	m.listener, err = net.ListenTCP("tcp4", &net.TCPAddr{Port: m.port})
	if err != nil {
		m.log.Errorf("cannot listen port %d: %s", m.port, err)
		return
	}

	m.log.Notice("Listening peers on tcp://" + m.listener.Addr().String())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.acceptor(stopC)
	}()
}

func (m *PeerManager) startDialer(stopC chan struct{}) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.dialer(stopC)
	}()
}

func (m *PeerManager) handleConnect(p *peer.Peer, stopC chan struct{}) {
	m.m.Lock()
	defer m.m.Unlock()
	if _, ok := m.peers[p.ID()]; ok {
		m.log.Warningln("peer already connected, dropping connection:", p.String())
		go m.closePeer(p)
		return
	}
	go m.waitDisconnect(p, stopC)
	m.peers[p.ID()] = p
	p.Run(stopC)
}

func (m *PeerManager) closePeer(p *peer.Peer) {
	err := p.Close()
	if err != nil {
		m.log.Error(err)
	}
}

func (m *PeerManager) handleDisconnect(p *peer.Peer) {
	m.m.Lock()
	defer m.m.Unlock()
	delete(m.peers, p.ID())
}

func (m *PeerManager) waitDisconnect(p *peer.Peer, stopC chan struct{}) {
	<-p.NotifyDisconnect()
	select {
	case m.peerDisconnected <- p:
	case <-stopC:
		return
	}
}

func (m *PeerManager) Peers() []*peer.Peer {
	m.m.Lock()
	defer m.m.Unlock()
	peers := make([]*peer.Peer, 0, len(m.peers))
	for _, v := range m.peers {
		peers = append(peers, v)
	}
	return peers
}
