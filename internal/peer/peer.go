package peer

import (
	"math"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peerconn"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/peersource"
	"github.com/cenkalti/rain/internal/pieceset"
	"github.com/rcrowley/go-metrics"
)

type Peer struct {
	*peerconn.Conn

	ConnectedAt time.Time

	Source peersource.Source

	Bitfield    *bitfield.Bitfield
	AllowedFast pieceset.PieceSet

	ID                [20]byte
	ExtensionsEnabled bool
	FastEnabled       bool
	EncryptionCipher  mse.CryptoMethod

	ClientInterested bool
	ClientChoking    bool
	PeerInterested   bool
	PeerChoking      bool

	OptimisticUnchoked bool

	// Snubbed means peer is sending pieces too slow.
	Snubbed bool

	Downloading bool

	downloadSpeed metrics.EWMA
	uploadSpeed   metrics.EWMA

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

func New(p *peerconn.Conn, source peersource.Source, id [20]byte, extensions [8]byte, cipher mse.CryptoMethod, snubTimeout time.Duration) *Peer {
	bf, _ := bitfield.NewBytes(extensions[:], 64)
	fastEnabled := bf.Test(61)
	extensionsEnabled := bf.Test(43)

	t := time.NewTimer(math.MaxInt64)
	t.Stop()
	return &Peer{
		Conn:              p,
		Source:            source,
		ConnectedAt:       time.Now(),
		ID:                id,
		ClientChoking:     true,
		PeerChoking:       true,
		ExtensionsEnabled: extensionsEnabled,
		FastEnabled:       fastEnabled,
		EncryptionCipher:  cipher,
		snubTimeout:       snubTimeout,
		snubTimer:         t,
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
		downloadSpeed:     metrics.NewEWMA1(),
		uploadSpeed:       metrics.NewEWMA1(),
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

func (p *Peer) Done() chan struct{} {
	return p.doneC
}

func (p *Peer) Run(messages chan Message, pieces chan interface{}, snubbed, disconnect chan *Peer) {
	defer close(p.doneC)
	go p.Conn.Run()

	speedTicker := time.NewTicker(5 * time.Second)
	defer speedTicker.Stop()

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
				p.downloadSpeed.Update(int64(len(m.Buffer.Data)))
				select {
				case pieces <- PieceMessage{Peer: p, Piece: m}:
				case <-p.closeC:
					return
				}
			} else {
				if m, ok := pm.(peerwriter.BlockUploaded); ok {
					p.uploadSpeed.Update(int64(m.Length))
				}
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
		case <-speedTicker.C:
			p.downloadSpeed.Tick()
			p.uploadSpeed.Tick()
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

func (p *Peer) DownloadSpeed() uint {
	return uint(p.downloadSpeed.Rate())
}

func (p *Peer) UploadSpeed() uint {
	return uint(p.uploadSpeed.Rate())
}

func (p *Peer) Choke() {
	p.ClientChoking = true
	p.SendMessage(peerprotocol.ChokeMessage{})
}

func (p *Peer) Unchoke() {
	p.ClientChoking = false
	p.SendMessage(peerprotocol.UnchokeMessage{})
}

func (p *Peer) Choking() bool {
	return p.ClientChoking
}

func (p *Peer) Interested() bool {
	return p.PeerInterested
}

func (p *Peer) Optimistic() bool {
	return p.OptimisticUnchoked
}

func (p *Peer) SetOptimistic(value bool) {
	p.OptimisticUnchoked = value
}

func (p *Peer) MetadataSize() uint32 {
	return uint32(p.ExtensionHandshake.MetadataSize)
}

func (p *Peer) RequestMetadataPiece(index uint32) {
	p.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata],
		Payload: peerprotocol.ExtensionMetadataMessage{
			Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
			Piece: index,
		},
	})
}

func (p *Peer) RequestPiece(index, begin, length uint32) {
	msg := peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length}
	p.SendMessage(msg)
}

func (p *Peer) CancelPiece(index, begin, length uint32) {
	msg := peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length}}
	p.SendMessage(msg)
}

func (p *Peer) EnabledFast() bool {
	return p.FastEnabled
}

func (p *Peer) Client() string {
	if p.ExtensionHandshake == nil {
		return ""
	}
	if p.ExtensionHandshake.V != "" {
		return p.ExtensionHandshake.V
	}
	return asciify(clientID(string(p.ID[:])))
}
