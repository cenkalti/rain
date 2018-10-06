package peer

import (
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/peerconn"
	"github.com/cenkalti/rain/internal/peerconn/peerprotocol"
)

type Peer struct {
	*peerconn.Peer
	messages   chan Message
	disconnect chan *Peer

	Downloader     *piecedownloader.PieceDownloader
	InfoDownloader *infodownloader.InfoDownloader

	AmChoking                    bool
	AmInterested                 bool
	PeerChoking                  bool
	PeerInterested               bool
	BytesDownlaodedInChokePeriod int64
	OptimisticUnhoked            bool

	// Messages received while we don't have info yet are saved here.
	Messages []interface{}

	ExtensionHandshake *peerprotocol.ExtensionHandshakeMessage

	closeC  chan struct{}
	closedC chan struct{}
}

type Message struct {
	*Peer
	Message interface{}
}

func New(p *peerconn.Peer, messages chan Message, disconnect chan *Peer) *Peer {
	return &Peer{
		Peer:        p,
		messages:    messages,
		disconnect:  disconnect,
		AmChoking:   true,
		PeerChoking: true,
		closeC:      make(chan struct{}),
		closedC:     make(chan struct{}),
	}
}

func (p *Peer) Close() {
	close(p.closeC)
	p.Peer.Close()
	<-p.closedC
}

func (p *Peer) Run() {
	defer close(p.closedC)
	go p.Peer.Run()
	for msg := range p.Peer.Messages() {
		select {
		case p.messages <- Message{Peer: p, Message: msg}:
		case <-p.closeC:
			return
		}
	}
	select {
	case p.disconnect <- p:
	case <-p.closeC:
		return
	}
}

type ByDownloadRate []*Peer

func (a ByDownloadRate) Len() int      { return len(a) }
func (a ByDownloadRate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByDownloadRate) Less(i, j int) bool {
	return a[i].BytesDownlaodedInChokePeriod > a[j].BytesDownlaodedInChokePeriod
}
