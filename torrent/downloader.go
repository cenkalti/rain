package torrent

// import (
// 	"github.com/cenkalti/rain/bitfield"
// 	"github.com/cenkalti/rain/piece"
// )

// type pieceState struct {
// 	p *piece.Piece
// 	requested map[*peer]
// }

// TODO implement
func (t *Torrent) downloader() {
	defer t.stopWG.Done()
	// have := t.bitfield
	// requested := bitfield.New(have.Len())
	// for {
	// 	i := t.bitfield.FirstClear()
	// 	for _, peer := range t.peers {
	// 		if peer.Has(i) {
	// 			peer.Request(i)
	// 		}
	// 	}
	// }
}
