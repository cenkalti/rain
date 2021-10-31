package torrent

import (
	"math"

	"github.com/cenkalti/rain/internal/tracker"
)

func (t *torrent) handleNewTrackers(trackers []tracker.Tracker) {
	t.trackers = append(t.trackers, trackers...)
	status := t.status()
	if status != Stopping && status != Stopped {
		for _, tr := range trackers {
			t.startNewAnnouncer(tr)
		}
	}
}

func (t *torrent) announcerFields() tracker.Torrent {
	tr := tracker.Torrent{
		InfoHash:        t.infoHash,
		PeerID:          t.peerID,
		Port:            t.port,
		BytesDownloaded: t.bytesDownloaded.Count(),
		BytesUploaded:   t.bytesUploaded.Count(),
	}
	// t.bytesComplete() uses t.bitfied for calculation.
	t.mBitfield.RLock()
	if t.bitfield == nil {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info.Length - t.bytesComplete()
	}
	t.mBitfield.RUnlock()
	return tr
}
