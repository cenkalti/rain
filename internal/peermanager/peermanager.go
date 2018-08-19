package peermanager

import (
	"net"
	"sync"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/torrentdata"
)

// Do not connect more than maxPeerPerTorrent peers.
const maxPeerPerTorrent = 60

type PeerManager struct {
	peers            map[[20]byte]*peer.Peer // connected peers
	peerConnected    chan *peer.Peer
	peerDisconnected chan *peer.Peer
	port             int
	listener         *net.TCPListener
	listenerReady    chan *net.TCPListener
	peerList         *peerlist.PeerList
	peerID           [20]byte
	infoHash         [20]byte
	sKeyHash         [20]byte
	data             *torrentdata.Data
	limiter          chan struct{}
	peerMessages     *peer.Messages
	log              logger.Logger
	wg               sync.WaitGroup
}

func New(port int, pl *peerlist.PeerList, peerID, infoHash [20]byte, d *torrentdata.Data, l logger.Logger) *PeerManager {
	return &PeerManager{
		peers:            make(map[[20]byte]*peer.Peer),
		peerConnected:    make(chan *peer.Peer),
		peerDisconnected: make(chan *peer.Peer),
		port:             port,
		listenerReady:    make(chan *net.TCPListener, 1),
		peerList:         pl,
		peerID:           peerID,
		infoHash:         infoHash,
		sKeyHash:         mse.HashSKey(infoHash[:]),
		data:             d,
		limiter:          make(chan struct{}, maxPeerPerTorrent),
		peerMessages:     peer.NewMessages(),
		log:              l,
	}
}

func (m *PeerManager) PeerMessages() *peer.Messages {
	return m.peerMessages
}

func (m *PeerManager) closeListener() {
	if m.listener == nil {
		return
	}
	err := m.listener.Close()
	if err != nil {
		m.log.Errorln("cannot close listener:", err)
	}
}

func (m *PeerManager) Run(stopC chan struct{}) {
	go m.createListener()
	m.startDialer(stopC)
	for {
		select {
		case m.listener = <-m.listenerReady:
			m.startAcceptor(stopC)
		case p := <-m.peerConnected:
			m.handleConnect(p, stopC)
		case p := <-m.peerDisconnected:
			m.handleDisconnect(p)
		case <-stopC:
			m.closeListener()
			m.wg.Wait()
			return
		}
	}
}

func (m *PeerManager) createListener() {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: m.port})
	if err != nil {
		m.log.Errorf("cannot listen port %d: %s", m.port, err)
		return
	}
	m.log.Notice("Listening peers on tcp://" + listener.Addr().String())
	m.listenerReady <- listener
}

func (m *PeerManager) startAcceptor(stopC chan struct{}) {
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
	if _, ok := m.peers[p.ID()]; ok {
		m.log.Warningln("peer already connected, dropping connection:", p.String())
		go m.closePeer(p)
		return
	}
	go m.waitDisconnect(p, stopC)
	m.peers[p.ID()] = p
	go func() {
		p.Run(stopC)
		m.wg.Done()
	}()
}

func (m *PeerManager) closePeer(p *peer.Peer) {
	err := p.Close()
	if err != nil {
		m.log.Error(err)
	}
	m.wg.Done()
}

func (m *PeerManager) handleDisconnect(p *peer.Peer) {
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
