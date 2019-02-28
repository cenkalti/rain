package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/verifier"
)

func (t *torrent) handleVerificationDone(ve *verifier.Verifier) {
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

	var haveMessages []peerprotocol.HaveMessage

	// Mark downloaded pieces.
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		if t.bitfield.Test(i) {
			t.pieces[i].Done = true
			haveMessages = append(haveMessages, peerprotocol.HaveMessage{Index: i})
		}
	}

	// Tell connected peers that pieces we have.
	for pe := range t.peers {
		for _, msg := range haveMessages {
			pe.SendMessage(msg)
		}
		t.updateInterestedState(pe)
	}

	t.checkCompletion()
	t.processQueuedMessages()
	t.startAcceptor()
	t.startAnnouncers()
	t.startPieceDownloaders()
}
