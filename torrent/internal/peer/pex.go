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
	pexTicker  *time.Ticker
	pexTickerC <-chan time.Time

	pexStartC    chan struct{}
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
		pexStartC:    make(chan struct{}),
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
	for {
		select {
		case addr := <-p.pexAddPeerC:
			p.pexList.Add(addr)
		case addr := <-p.pexDropPeerC:
			p.pexList.Drop(addr)
		case <-p.pexStartC:
			p.pexStartC = nil
			p.pexFlushPeers()
			p.pexTicker = time.NewTicker(time.Minute)
			p.pexTickerC = p.pexTicker.C
			defer p.pexTicker.Stop()
		case <-p.pexTickerC:
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
