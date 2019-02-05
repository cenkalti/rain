package torrent

import (
	"math"

	"github.com/cenkalti/rain/internal/tracker"
)

func (t *torrent) announcerFields() tracker.Torrent {
	tr := tracker.Torrent{
		InfoHash:        t.infoHash,
		PeerID:          t.peerID,
		Port:            t.port,
		BytesDownloaded: t.resumerStats.BytesDownloaded,
		BytesUploaded:   t.resumerStats.BytesUploaded,
	}
	if t.bitfield == nil {
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info.TotalLength - t.bytesComplete()
	}
	return tr
}
