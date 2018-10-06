package piece

import (
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	pp "github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	*pp.Piece
	HavingPeers      map[*peerconn.Conn]struct{}
	AllowedFastPeers map[*peerconn.Conn]struct{}
	RequestedPeers   map[*peerconn.Conn]*piecedownloader.PieceDownloader
	Writing          bool
}

func New(p *pp.Piece) Piece {
	return Piece{
		Piece:            p,
		HavingPeers:      make(map[*peerconn.Conn]struct{}),
		AllowedFastPeers: make(map[*peerconn.Conn]struct{}),
		RequestedPeers:   make(map[*peerconn.Conn]*piecedownloader.PieceDownloader),
	}
}

type ByAvailability []*Piece

func (a ByAvailability) Len() int           { return len(a) }
func (a ByAvailability) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAvailability) Less(i, j int) bool { return len(a[i].HavingPeers) < len(a[j].HavingPeers) }
