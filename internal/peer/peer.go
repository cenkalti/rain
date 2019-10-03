package peer

import (
	"math"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/fast"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peerconn"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/peersource"
	"github.com/cenkalti/rain/internal/pexlist"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/pieceset"
	"github.com/cenkalti/rain/internal/stringutil"
	"github.com/juju/ratelimit"
	"github.com/rcrowley/go-metrics"
)

// Peer of a Torrent. Wraps a BitTorrent connection.
type Peer struct {
	*peerconn.Conn

	ConnectedAt time.Time

	Source peersource.Source

	Bitfield            *bitfield.Bitfield
	ReceivedAllowedFast pieceset.PieceSet
	SentAllowedFast     pieceset.PieceSet

	ID                [20]byte
	ExtensionsEnabled bool
	FastEnabled       bool
	DHTEnabled        bool
	EncryptionCipher  mse.CryptoMethod

	ClientInterested bool
	ClientChoking    bool
	PeerInterested   bool
	PeerChoking      bool

	OptimisticUnchoked bool

	// Snubbed means peer is sending pieces too slow.
	Snubbed bool

	Downloading bool

	downloadSpeed metrics.Meter
	uploadSpeed   metrics.Meter

	// Messages received while we don't have info yet are saved here.
	Messages []interface{}

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	PEX *pex

	snubTimeout time.Duration
	snubTimer   *time.Timer

	closeC chan struct{}
	doneC  chan struct{}
}

// Message that is read from Peer
type Message struct {
	*Peer
	Message interface{}
}

// PieceMessage is a Piece message that is read from Peer
type PieceMessage struct {
	*Peer
	Piece peerreader.Piece
}

// New wraps the net.Conn and returns a new Peer.
func New(conn net.Conn, source peersource.Source, id [20]byte, extensions [8]byte, cipher mse.CryptoMethod, pieceReadTimeout, snubTimeout time.Duration, maxRequestsIn int, br, bw *ratelimit.Bucket) *Peer {
	bf, _ := bitfield.NewBytes(extensions[:], 64)
	fastEnabled := bf.Test(61)
	extensionsEnabled := bf.Test(43)
	dhtEnabled := bf.Test(63)

	t := time.NewTimer(math.MaxInt64)
	t.Stop()
	return &Peer{
		Conn:              peerconn.New(conn, newPeerLogger(source, conn), pieceReadTimeout, maxRequestsIn, fastEnabled, br, bw),
		Source:            source,
		ConnectedAt:       time.Now(),
		ID:                id,
		ClientChoking:     true,
		PeerChoking:       true,
		ExtensionsEnabled: extensionsEnabled,
		FastEnabled:       fastEnabled,
		DHTEnabled:        dhtEnabled,
		EncryptionCipher:  cipher,
		snubTimeout:       snubTimeout,
		snubTimer:         t,
		closeC:            make(chan struct{}),
		doneC:             make(chan struct{}),
		downloadSpeed:     metrics.NewMeter(),
		uploadSpeed:       metrics.NewMeter(),
	}
}

func newPeerLogger(src peersource.Source, conn net.Conn) logger.Logger {
	if src == peersource.Incoming {
		return logger.New("peer <- " + conn.RemoteAddr().String())
	}
	return logger.New("peer -> " + conn.RemoteAddr().String())
}

// Close the peer connection.
func (p *Peer) Close() {
	p.snubTimer.Stop()
	if p.PEX != nil {
		p.PEX.close()
	}
	close(p.closeC)
	p.Conn.Close()
	p.downloadSpeed.Stop()
	p.uploadSpeed.Stop()
	<-p.doneC
}

// Done returns a channel that is closed when a peers run loop is ended.
func (p *Peer) Done() chan struct{} {
	return p.doneC
}

// Run loop that reads messages from the Peer.
func (p *Peer) Run(messages chan Message, pieces chan interface{}, snubbed, disconnect chan *Peer) {
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
				p.downloadSpeed.Mark(int64(len(m.Buffer.Data)))
				select {
				case pieces <- PieceMessage{Peer: p, Piece: m}:
				case <-p.closeC:
					return
				}
			} else {
				if m, ok := pm.(peerwriter.BlockUploaded); ok {
					p.uploadSpeed.Mark(int64(m.Length))
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
		case <-p.closeC:
			return
		}
	}
}

// StartPEX starts the PEX goroutine for sending PEX messages to the Peer periodically.
func (p *Peer) StartPEX(initialPeers map[*Peer]struct{}, recentlySeen *pexlist.RecentlySeen) {
	if p.PEX == nil {
		p.PEX = newPEX(p.Conn, p.ExtensionHandshake.M[peerprotocol.ExtensionKeyPEX], initialPeers, recentlySeen)
		go p.PEX.run()
	}
}

// ResetSnubTimer is called when some data received from the Peer.
func (p *Peer) ResetSnubTimer() {
	p.snubTimer.Reset(p.snubTimeout)
}

// StopSnubTimer is used to stop the timer that is for detecting if the Peer is snub.
func (p *Peer) StopSnubTimer() {
	p.snubTimer.Stop()
}

// DownloadSpeed of the Peer in bytes per second.
func (p *Peer) DownloadSpeed() int {
	return int(p.downloadSpeed.Rate1())
}

// UploadSpeed of the Peer in bytes per second.
func (p *Peer) UploadSpeed() int {
	return int(p.uploadSpeed.Rate1())
}

// Choke the connected Peer by sending a "choke" protocol message.
func (p *Peer) Choke() {
	p.ClientChoking = true
	p.SendMessage(peerprotocol.ChokeMessage{})
}

// Unchoke the connected Peer by sending an "unchoke" protocol message.
func (p *Peer) Unchoke() {
	p.ClientChoking = false
	p.SendMessage(peerprotocol.UnchokeMessage{})
}

// Choking returns true if we are choking the remote Peer.
func (p *Peer) Choking() bool {
	return p.ClientChoking
}

// Interested returns true if remote Peer is interested for pieces we have.
func (p *Peer) Interested() bool {
	return p.PeerInterested
}

// Optimistic returns true if we are unchoking the Peer optimistically.
func (p *Peer) Optimistic() bool {
	return p.OptimisticUnchoked
}

// SetOptimistic sets the status of if we are chiking the peer optimistically.
func (p *Peer) SetOptimistic(value bool) {
	p.OptimisticUnchoked = value
}

// MetadataSize returns the torrent metadata size that is received from the Peer with an extension handshake message.
func (p *Peer) MetadataSize() uint32 {
	return uint32(p.ExtensionHandshake.MetadataSize)
}

// RequestMetadataPiece is used to send a message that is requesting a metadata piece at index.
func (p *Peer) RequestMetadataPiece(index uint32) {
	p.SendMessage(peerprotocol.ExtensionMessage{
		ExtendedMessageID: p.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata],
		Payload: peerprotocol.ExtensionMetadataMessage{
			Type:  peerprotocol.ExtensionMetadataMessageTypeRequest,
			Piece: index,
		},
	})
}

// RequestPiece is used to request a piece at index by sending a "piece" protocol message.
func (p *Peer) RequestPiece(index, begin, length uint32) {
	msg := peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length}
	p.SendMessage(msg)
}

// CancelPiece cancels previosly requested piece. Sends "cancel" protocol message.
func (p *Peer) CancelPiece(index, begin, length uint32) {
	msg := peerprotocol.CancelMessage{RequestMessage: peerprotocol.RequestMessage{Index: index, Begin: begin, Length: length}}
	p.SendMessage(msg)
}

// EnabledFast returns true if the remote Peer supports Fast extension.
func (p *Peer) EnabledFast() bool {
	return p.FastEnabled
}

// Client returns the name of the client.
// Returns client string in extension handshake. If extension handshake is not done, returns asciified version of the peer ID.
func (p *Peer) Client() string {
	if p.ExtensionHandshake != nil && p.ExtensionHandshake.V != "" {
		return stringutil.Printable(p.ExtensionHandshake.V)
	}
	return stringutil.Asciify(clientID(string(p.ID[:])))
}

// GenerateAndSendAllowedFastMessages is used to send "allowed fast" protocol messages after handshake.
func (p *Peer) GenerateAndSendAllowedFastMessages(k int, numPieces uint32, infoHash [20]byte, pieces []piece.Piece) {
	if k == 0 {
		return
	}
	if p.SentAllowedFast.Len() > 0 {
		return
	}
	a := fast.GenerateFastSet(k, numPieces, infoHash, p.Conn.Addr().IP)
	for _, index := range a {
		p.SentAllowedFast.Add(&pieces[index])
		p.SendMessage(peerprotocol.AllowedFastMessage{HaveMessage: peerprotocol.HaveMessage{Index: index}})
	}
}
