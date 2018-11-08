package peer

import (
	"time"

	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
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

	PEX *pex

	snubTimer       *time.Timer
	snubTimerCloseC chan struct{}

	closeC chan struct{}
	doneC  chan struct{}
}

type Message struct {
	*Peer
	Message interface{}
}

func New(p *peerconn.Conn) *Peer {
	return &Peer{
		Conn:              p,
		AmChoking:         true,
		PeerChoking:       true,
		AllowedFastPieces: make(map[uint32]struct{}),
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

func (p *Peer) Close() {
	if p.PEX != nil {
		p.PEX.Close()
	}
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
		case <-p.closeC:
			return
		}
	}
}

func (p *Peer) StartPEX(initialPeers map[*Peer]struct{}) {
	p.PEX = newPEX(p.Conn, p.ExtensionHandshake.M[peerprotocol.ExtensionKeyPEX], initialPeers)
	go p.PEX.Run()
}

func (p *Peer) StartSnubTimer(d time.Duration, snubbedC chan *Peer) {
	p.StopSnubTimer()
	p.snubTimer = time.NewTimer(d)
	p.snubTimerCloseC = make(chan struct{})
	go p.waitSnubTimeout(p.snubTimer, snubbedC, p.closeC)
}

func (p *Peer) waitSnubTimeout(t *time.Timer, snubbedC chan *Peer, closeC chan struct{}) {
	select {
	case <-t.C:
		snubbedC <- p
	case <-closeC:
	}
}

func (p *Peer) StopSnubTimer() {
	if p.snubTimer != nil {
		p.snubTimer.Stop()
		p.snubTimer = nil
		close(p.snubTimerCloseC)
		p.snubTimerCloseC = nil
	}
}
