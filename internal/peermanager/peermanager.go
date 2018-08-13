package peermanager

import (
	"net"
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
)

const (
	// Do not connect more than maxPeerPerTorrent peers.
	maxPeerPerTorrent = 60
)

type PeerManager struct {
	peers            map[[20]byte]*peer.Peer // connected peers
	peerConnected    chan *peer.Peer
	peerDisconnected chan *peer.Peer
	listener         *net.TCPListener
	announcer        *announcer.Announcer
	peerID           [20]byte
	infoHash         [20]byte
	sKeyHash         [20]byte
	bitfield         *bitfield.Bitfield
	limiter          chan struct{}
	peerMessages     chan peer.Message
	log              logger.Logger
	m                sync.Mutex
	wg               sync.WaitGroup
}

func New(listener *net.TCPListener, a *announcer.Announcer, peerID, infoHash [20]byte, b *bitfield.Bitfield, l logger.Logger) *PeerManager {
	return &PeerManager{
		peers:            make(map[[20]byte]*peer.Peer),
		peerConnected:    make(chan *peer.Peer),
		peerDisconnected: make(chan *peer.Peer),
		listener:         listener,
		announcer:        a,
		peerID:           peerID,
		infoHash:         infoHash,
		sKeyHash:         mse.HashSKey(infoHash[:]),
		bitfield:         b,
		limiter:          make(chan struct{}, maxPeerPerTorrent),
		peerMessages:     make(chan peer.Message),
		log:              l,
	}
}

func (m *PeerManager) PeerMessages() chan peer.Message {
	return m.peerMessages
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
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		m.acceptor(stopC)
	}()
	go func() {
		defer m.wg.Done()
		m.dialer(stopC)
	}()
	for {
		select {
		case p := <-m.peerConnected:
			m.handleConnect(p, stopC)
		case p := <-m.peerDisconnected:
			m.handleDisconnect(p)
		case <-stopC:
			m.wg.Done()
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
