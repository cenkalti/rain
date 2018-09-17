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

	// TODO process saved messages after getting info.
	// Save space for some messages that is received while info does not exist.
	haveMessages        []peerprotocol.HaveMessage
	bitfieldMessage     []byte
	haveAllMessage      bool
	allowedFastMessages []peerprotocol.HaveMessage
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
