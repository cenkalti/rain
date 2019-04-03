package torrent

import (
	"fmt"
	"time"

	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
)

func (t *torrent) deferWriteBitfield() {
	if t.resumeWriteTimer == nil {
		t.resumeWriteTimer = time.NewTimer(t.session.config.BitfieldWriteInterval)
		t.resumeWriteTimerC = t.resumeWriteTimer.C
	}
}

func (t *torrent) writeBitfield(stopOnError bool) {
	if t.resumeWriteTimer != nil {
		t.resumeWriteTimer.Stop()
		t.resumeWriteTimer = nil
		t.resumeWriteTimerC = nil
	}
	err := t.resume.WriteBitfield(t.bitfield.Bytes())
	if err != nil {
		err = fmt.Errorf("cannot write bitfield to resume db: %s", err)
		t.log.Errorln(err)
		if stopOnError {
			t.stop(err)
		}
	}
}

func (t *torrent) checkCompletion() bool {
	if t.completed {
		return true
	}
	if !t.bitfield.All() {
		return false
	}
	t.completed = true
	close(t.completeC)
	for h := range t.outgoingHandshakers {
		h.Close()
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
	for _, src := range t.webseedSources {
		t.closeWebseedDownloader(src)
	}
	for pe := range t.peers {
		if !pe.PeerInterested {
			t.closePeer(pe)
		}
	}
	t.addrList.Reset()
	for _, pd := range t.pieceDownloaders {
		t.closePieceDownloader(pd)
		pd.CancelPending()
	}
	t.piecePicker = nil
	t.updateSeedDuration(time.Now())
	return true
}
