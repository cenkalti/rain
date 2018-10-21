package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/piece"
)

func (t *Torrent) handleAllocationDone(al *allocator.Allocator) {
	t.allocator = nil
	if al.Error != nil {
		t.stop(fmt.Errorf("file allocation error: %s", al.Error))
		return
	}
	t.data = al.Data
	t.pieces = make([]*piece.Piece, len(t.data.Pieces))
	t.sortedPieces = make([]*piece.Piece, len(t.data.Pieces))
	for i := range t.data.Pieces {
		p := piece.New(&t.data.Pieces[i])
		t.pieces[i] = p
		t.sortedPieces[i] = p
	}
	if t.bitfield != nil {
		t.checkCompletion()
		t.processQueuedMessages()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPieceDownloaders()
		t.startUnchokeTimers()
		return
	}
	if !al.NeedHashCheck {
		t.bitfield = bitfield.New(t.info.NumPieces)
		t.processQueuedMessages()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPieceDownloaders()
		t.startUnchokeTimers()
		return
	}
	t.startVerifier()
}
