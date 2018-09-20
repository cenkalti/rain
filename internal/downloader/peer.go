package downloader

import (
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

type Peer struct {
	*peer.Peer

	downloader     *piecedownloader.PieceDownloader
	infoDownloader *infodownloader.InfoDownloader

	amChoking                    bool
	amInterested                 bool
	peerChoking                  bool
	peerInterested               bool
	bytesDownlaodedInChokePeriod int64
	optimisticUnhoked            bool

	// Messages received while we don't have info yet are saved here.
	messages []interface{}

	extensionHandshake *peerprotocol.ExtensionHandshakeMessage
}

func NewPeer(p *peer.Peer) *Peer {
	return &Peer{
		Peer:        p,
		amChoking:   true,
		peerChoking: true,
	}
}

type ByDownloadRate []*Peer

func (a ByDownloadRate) Len() int      { return len(a) }
func (a ByDownloadRate) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByDownloadRate) Less(i, j int) bool {
	return a[i].bytesDownlaodedInChokePeriod > a[j].bytesDownlaodedInChokePeriod
}
