package torrent

import (
	"math"
	"sync/atomic"

	"github.com/cenkalti/rain/internal/tracker"
)

func (t *torrent) announcerFields() tracker.Torrent {
	tr := tracker.Torrent{
		InfoHash:        t.infoHash,
		PeerID:          t.peerID,
		Port:            t.port,
		BytesDownloaded: atomic.LoadInt64(&t.resumerStats.BytesDownloaded),
		BytesUploaded:   atomic.LoadInt64(&t.resumerStats.BytesUploaded),
	}
	t.mBitfield.RLock()
	if t.bitfield == nil {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info.TotalLength - t.bytesComplete()
	}
	t.mBitfield.RUnlock()
	return tr
}
