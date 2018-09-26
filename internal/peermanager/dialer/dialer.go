package dialer

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager/dialer/handler"
	"github.com/cenkalti/rain/internal/worker"
)

type Dialer struct {
	addrToCon   chan *net.TCPAddr
	peerID      [20]byte
	infoHash    [20]byte
	newPeers    chan *peer.Peer
	connectC    chan net.Conn
	disconnectC chan net.Conn
	workers     worker.Workers
	log         logger.Logger
}

func New(addrToCon chan *net.TCPAddr, peerID, infoHash [20]byte, newPeers chan *peer.Peer, connectC, disconnectC chan net.Conn, l logger.Logger) *Dialer {
	return &Dialer{
		addrToCon:   addrToCon,
		peerID:      peerID,
		infoHash:    infoHash,
		newPeers:    newPeers,
		connectC:    connectC,
		disconnectC: disconnectC,
		log:         l,
	}
}

func (d *Dialer) Run(stopC chan struct{}) {
	for {
		select {
		case addr := <-d.addrToCon:
			h := handler.New(addr, d.peerID, d.infoHash, d.newPeers, d.connectC, d.disconnectC, d.log)
			d.workers.Start(h)
		case <-stopC:
			d.workers.Stop()
			return
		}
	}
}
