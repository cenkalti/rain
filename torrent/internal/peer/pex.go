package peer

import (
	"net"
	"time"

	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pexlist"
)

type pex struct {
	conn  *peerconn.Conn
	extID uint8

	// Contains added and dropped peers.
	pexList *pexlist.PEXList

	// To send connected peers at interval
	pexTicker *time.Ticker

	pexAddPeerC  chan *net.TCPAddr
	pexDropPeerC chan *net.TCPAddr

	closeC chan struct{}
	doneC  chan struct{}
}

func newPEX(conn *peerconn.Conn, extID uint8, initialPeers map[*Peer]struct{}) *pex {
	pl := pexlist.New()
	for pe := range initialPeers {
		pl.Add(pe.Addr())
	}
	return &pex{
		conn:         conn,
		extID:        extID,
		pexList:      pl,
		pexAddPeerC:  make(chan *net.TCPAddr),
		pexDropPeerC: make(chan *net.TCPAddr),
		closeC:       make(chan struct{}),
		doneC:        make(chan struct{}),
	}
}

func (p *pex) Close() {
	close(p.closeC)
	<-p.doneC
}

func (p *pex) Run() {
	defer close(p.doneC)

	p.pexFlushPeers()

	p.pexTicker = time.NewTicker(time.Minute)
	defer p.pexTicker.Stop()

	for {
		select {
		case addr := <-p.pexAddPeerC:
			p.pexList.Add(addr)
		case addr := <-p.pexDropPeerC:
			p.pexList.Drop(addr)
		case <-p.pexTicker.C:
			p.pexFlushPeers()
		case <-p.closeC:
			return
		}
	}
}

func (p *pex) Add(addr *net.TCPAddr) {
	select {
	case p.pexAddPeerC <- addr:
	case <-p.doneC:
	}
}

func (p *pex) Drop(addr *net.TCPAddr) {
	select {
	case p.pexDropPeerC <- addr:
	case <-p.doneC:
	}
}

func (p *pex) pexFlushPeers() {
	added, dropped := p.pexList.Flush()
	extPEXMsg := peerprotocol.ExtensionPEXMessage{
		Added:   added,
		Dropped: dropped,
	}
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.extID,
		Payload:           extPEXMsg,
	}
	p.conn.SendMessage(msg)
}
