package piece

import (
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	*pieceio.Piece
	// TODO Piece has no state. Move these maps into downloader.
	HavingPeers      map[*peer.Peer]struct{}
	AllowedFastPeers map[*peer.Peer]struct{}
	RequestedPeers   map[*peer.Peer]*piecedownloader.PieceDownloader
}

func New(p *pieceio.Piece) *Piece {
	return &Piece{
		Piece:            p,
		HavingPeers:      make(map[*peer.Peer]struct{}),
		AllowedFastPeers: make(map[*peer.Peer]struct{}),
		RequestedPeers:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
	}
}

func (p *Piece) RunningDownloads() int {
	n := len(p.RequestedPeers)
	for pe := range p.RequestedPeers {
		if pe.Snubbed {
			n--
		}
	}
	return n
}

type ByAvailability []*Piece

func (a ByAvailability) Len() int           { return len(a) }
func (a ByAvailability) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAvailability) Less(i, j int) bool { return len(a[i].HavingPeers) < len(a[j].HavingPeers) }
