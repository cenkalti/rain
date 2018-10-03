package peer

import (
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	pp "github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

type Peer struct {
	*pp.Peer

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
}

func New(p *pp.Peer) *Peer {
	return &Peer{
		Peer:        p,
		AmChoking:   true,
		PeerChoking: true,
	}
}

type ByDownloadRate []*Peer

func (a ByDownloadRate) Len() int      { return len(a) }
func (a ByDownloadRate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByDownloadRate) Less(i, j int) bool {
	return a[i].BytesDownlaodedInChokePeriod > a[j].BytesDownlaodedInChokePeriod
}
