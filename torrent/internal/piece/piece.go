package piece

import (
	"github.com/cenkalti/rain/torrent/internal/peer"
	pp "github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	*pp.Piece
	HavingPeers      map[*peer.Peer]struct{}
	AllowedFastPeers map[*peer.Peer]struct{}
}

func New(p *pp.Piece) *Piece {
	return &Piece{
		Piece:            p,
		HavingPeers:      make(map[*peer.Peer]struct{}),
		AllowedFastPeers: make(map[*peer.Peer]struct{}),
	}
}

type ByAvailability []*Piece

func (a ByAvailability) Len() int           { return len(a) }
func (a ByAvailability) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAvailability) Less(i, j int) bool { return len(a[i].HavingPeers) < len(a[j].HavingPeers) }
