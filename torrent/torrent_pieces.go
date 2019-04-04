package torrent

import (
	"fmt"
	"time"

	"github.com/boltdb/bolt"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/resumer/boltdbresumer"
)

func (t *torrent) writeBitfield() error {
	err := t.session.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.id))
		if b == nil {
			return nil
		}
		return b.Put(boltdbresumer.Keys.Bitfield, t.bitfield.Bytes())
	})
	if err != nil {
		err = fmt.Errorf("cannot write bitfield to resume db: %s", err)
		t.log.Errorln(err)
	}
	return err
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
