package peer

import (
	"net"
	"time"

	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pexlist"
)

type Peer struct {
	*peerconn.Conn

	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool

	OptimisticUnchoked bool

	// Snubbed means peer is sending pieces too slow.
	Snubbed bool

	BytesDownlaodedInChokePeriod int64
	BytesUploadedInChokePeriod   int64

	// Messages received while we don't have info yet are saved here.
	Messages []interface{}

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	AllowedFastPieces map[uint32]struct{}

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

type Message struct {
	*Peer
	Message interface{}
}

func New(p *peerconn.Conn, pexInitialPeers map[*Peer]struct{}) *Peer {
	pl := pexlist.New()
	for pe := range pexInitialPeers {
		pl.Add(pe.Addr())
	}
	return &Peer{
		Conn:              p,
		AmChoking:         true,
		PeerChoking:       true,
		AllowedFastPieces: make(map[uint32]struct{}),
		pexList:           pl,
		pexStartC:         make(chan struct{}),
		pexAddPeerC:       make(chan *net.TCPAddr),
		pexDropPeerC:      make(chan *net.TCPAddr),
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

func (p *Peer) Close() {
	close(p.closeC)
	p.Conn.Close()
	<-p.doneC
}

func (p *Peer) Run(messages chan Message, disconnect chan *Peer) {
	defer close(p.doneC)
	go p.Conn.Run()
	for {
		select {
		case pm, ok := <-p.Conn.Messages():
			if !ok {
				select {
				case disconnect <- p:
				case <-p.closeC:
				}
				return
			}
			select {
			case messages <- Message{Peer: p, Message: pm}:
			case <-p.closeC:
				return
			}
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

func (p *Peer) PEXStart() {
	close(p.pexStartC)
}

func (p *Peer) PEXAdd(addr *net.TCPAddr) {
	select {
	case p.pexAddPeerC <- addr:
	case <-p.doneC:
	}
}

func (p *Peer) PEXDrop(addr *net.TCPAddr) {
	select {
	case p.pexDropPeerC <- addr:
	case <-p.doneC:
	}
}

func (p *Peer) pexFlushPeers() {
	added, dropped := p.pexList.Flush()
	extPEXMsg := peerprotocol.ExtensionPEXMessage{
		Added:   added,
		Dropped: dropped,
	}
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.ExtensionHandshake.M[peerprotocol.ExtensionKeyPEX],
		Payload:           extPEXMsg,
	}
	p.SendMessage(msg)
}
