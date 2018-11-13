package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecepicker"
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

	t.files = al.Files
	t.pieces = piece.NewPieces(t.info, t.files)
	if t.piecePicker != nil {
		panic("piece picker exists")
	}
	t.piecePicker = piecepicker.New(t.info.NumPieces, t.config.EndgameParallelDownloadsPerPiece, t.log)

	// If we already have bitfield from resume db, skip verification and start downloading.
	if t.bitfield != nil {
		t.checkCompletion()
		t.processQueuedMessages()
		t.startAcceptor()
		t.startAnnouncers()
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
		t.startPieceDownloaders()
		t.startUnchokeTimers()
		return
	}

	// Some files exists on the disk, need to verify pieces to create a correct bitfield.
	t.startVerifier()
}
