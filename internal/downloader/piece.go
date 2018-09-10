package downloader

import (
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type Piece struct {
	*piece.Piece
	havingPeers      map[*peer.Peer]struct{}
	allowedFastPeers map[*peer.Peer]struct{}
	requestedPeers   map[*peer.Peer]*piecedownloader.PieceDownloader
	writing          bool
}

type ByAvailability []*Piece

func (a ByAvailability) Len() int           { return len(a) }
func (a ByAvailability) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByAvailability) Less(i, j int) bool { return len(a[i].havingPeers) < len(a[j].havingPeers) }
