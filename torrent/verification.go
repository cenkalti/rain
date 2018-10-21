package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/verifier"
)

func (t *Torrent) handleVerificationDone(ve *verifier.Verifier) {
	t.verifier = nil
	if ve.Error != nil {
		t.stop(fmt.Errorf("file verification error: %s", ve.Error))
		return
	}
	t.bitfield = ve.Bitfield
	if t.resume != nil {
		err := t.resume.WriteBitfield(t.bitfield.Bytes())
		if err != nil {
			t.stop(fmt.Errorf("cannot write bitfield to resume db: %s", err))
			return
		}
	}
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
	t.startPieceDownloaders()
	t.startUnchokeTimers()
}
