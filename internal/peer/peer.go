package peer

import (
	"math"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peerconn"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerprotocol"
)

type Peer struct {
	*peerconn.Conn

	Source Source

	ID                [20]byte
	ExtensionsEnabled bool
	FastEnabled       bool
	EncryptionCipher  mse.CryptoMethod

	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool

	OptimisticUnchoked bool

	// Snubbed means peer is sending pieces too slow.
	Snubbed bool

	Downloading bool

	BytesDownlaodedInChokePeriod int64
	BytesUploadedInChokePeriod   int64

	// Messages received while we don't have info yet are saved here.
	Messages []interface{}

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	PEX *pex

	snubTimeout time.Duration
	snubTimer   *time.Timer

	closeC chan struct{}
	doneC  chan struct{}
}

type Message struct {
	*Peer
	Message interface{}
}

type PieceMessage struct {
	*Peer
	Piece peerreader.Piece
}

func New(p *peerconn.Conn, source Source, id [20]byte, extensions [8]byte, cipher mse.CryptoMethod, snubTimeout time.Duration) *Peer {
	bf, _ := bitfield.NewBytes(extensions[:], 64)
	fastEnabled := bf.Test(61)
	extensionsEnabled := bf.Test(43)

	t := time.NewTimer(math.MaxInt64)
	t.Stop()
	return &Peer{
		Conn:              p,
		Source:            source,
		ID:                id,
		AmChoking:         true,
		PeerChoking:       true,
		ExtensionsEnabled: extensionsEnabled,
		FastEnabled:       fastEnabled,
		EncryptionCipher:  cipher,
		snubTimeout:       snubTimeout,
		snubTimer:         t,
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

func (p *Peer) Close() {
	p.snubTimer.Stop()
	if p.PEX != nil {
		p.PEX.close()
	}
	close(p.closeC)
	p.Conn.Close()
	<-p.doneC
}

func (p *Peer) Run(messages chan Message, pieces chan PieceMessage, snubbed, disconnect chan *Peer) {
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
			if m, ok := pm.(peerreader.Piece); ok {
				select {
				case pieces <- PieceMessage{Peer: p, Piece: m}:
				case <-p.closeC:
					return
				}
			} else {
				select {
				case messages <- Message{Peer: p, Message: pm}:
				case <-p.closeC:
					return
				}
			}
		case <-p.snubTimer.C:
			select {
			case snubbed <- p:
			case <-p.closeC:
				return
			}
		case <-p.closeC:
			return
		}
	}
}

func (p *Peer) StartPEX(initialPeers map[*Peer]struct{}) {
	if p.PEX == nil {
		p.PEX = newPEX(p.Conn, p.ExtensionHandshake.M[peerprotocol.ExtensionKeyPEX], initialPeers)
		go p.PEX.run()
	}
}

func (p *Peer) ResetSnubTimer() {
	p.snubTimer.Reset(p.snubTimeout)
}

func (p *Peer) StopSnubTimer() {
	p.snubTimer.Stop()
}
