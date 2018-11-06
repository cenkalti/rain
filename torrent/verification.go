package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/verifier"
)

func (t *Torrent) handleVerificationDone(ve *verifier.Verifier) {
	if t.verifier != ve {
		panic("invalid verifier")
	}
	t.verifier = nil

	if ve.Error != nil {
		t.stop(fmt.Errorf("file verification error: %s", ve.Error))
		return
	}

	// Now we have a constructed and verified bitfield.
	t.bitfield = ve.Bitfield

	// Save the bitfield to resume db.
	if t.resume != nil {
		err := t.resume.WriteBitfield(t.bitfield.Bytes())
		if err != nil {
			t.stop(fmt.Errorf("cannot write bitfield to resume db: %s", err))
			return
		}
	}

	// Tell connected peers that pieces we have.
	for pe := range t.peers {
		for i := uint32(0); i < t.bitfield.Len(); i++ {
			if t.bitfield.Test(i) {
				msg := peerprotocol.HaveMessage{Index: i}
				pe.SendMessage(msg)
			}
		}
		t.updateInterestedState(pe)
	}

	t.checkCompletion()
	t.processQueuedMessages()
	t.startAcceptor()
	t.startAnnouncers()
	t.startPEXTimer()
	t.startPieceDownloaders()
	t.startUnchokeTimers()
}
