package piece

import (
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	*pieceio.Piece
}

func New(p *pieceio.Piece) *Piece {
	return &Piece{
		Piece: p,
	}
}
