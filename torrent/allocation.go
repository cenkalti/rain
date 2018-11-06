package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/piece"
)

func (t *Torrent) handleAllocationDone(al *allocator.Allocator) {
	if t.allocator != al {
		panic("invalid allocator")
	}
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

	// If we already have bitfield from resume db, skip verification and start downloading.
	if t.bitfield != nil {
		t.checkCompletion()
		t.processQueuedMessages()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPEXTimer()
		t.startPieceDownloaders()
		t.startUnchokeTimers()
		return
	}

	// No need to verify files if they didn't exist when we create them.
	if !al.NeedHashCheck {
		t.bitfield = bitfield.New(t.info.NumPieces)
		t.processQueuedMessages()
		t.startAcceptor()
		t.startAnnouncers()
		t.startPEXTimer()
		t.startPieceDownloaders()
		t.startUnchokeTimers()
		return
	}

	// Some files exists on the disk, need to verify pieces to create a correct bitfield.
	t.startVerifier()
}
