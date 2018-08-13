package peermanager

import (
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
)

type PeerManager struct {
	peers            map[[20]byte]*peer.Peer // connected peers
	peerConnected    chan *peer.Peer
	peerDisconnected chan *peer.Peer
	log              logger.Logger
	m                sync.Mutex
}

func New(l logger.Logger) *PeerManager {
	return &PeerManager{
		peers:            make(map[[20]byte]*peer.Peer),
		peerConnected:    make(chan *peer.Peer),
		peerDisconnected: make(chan *peer.Peer),
		log:              l,
	}
}

func (m *PeerManager) PeerConnected() chan *peer.Peer {
	return m.peerConnected
}

func (m *PeerManager) Close() error {
	m.m.Lock()
	defer m.m.Unlock()
	var result error
	for _, p := range m.peers {
		err := p.Close()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result
}

func (m *PeerManager) Run(stopC chan struct{}) {
	for {
		select {
		case p := <-m.peerConnected:
			m.handleConnect(p, stopC)
		case p := <-m.peerDisconnected:
			m.handleDisconnect(p)
		case <-stopC:
			return
		}
	}
}

func (m *PeerManager) handleConnect(p *peer.Peer, stopC chan struct{}) {
	m.m.Lock()
	defer m.m.Unlock()
	if _, ok := m.peers[p.ID()]; ok {
		m.log.Warningln("peer already connected, dropping connection:", p.String())
		err := p.Close()
		if err != nil {
			m.log.Error(err)
		}
		return
	}
	go m.waitDisconnect(p, stopC)
	m.peers[p.ID()] = p
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
