package torrent

import (
	"math"

	"github.com/cenkalti/rain/torrent/internal/tracker"
)

func (t *Torrent) announcerFields() tracker.Transfer {
	tr := tracker.Transfer{
		InfoHash: t.infoHash,
		PeerID:   t.peerID,
		Port:     t.port,
		// TODO send BytesDownloaded to tracker
		// TODO send BytesUploaded to tracker
	}
	if t.bitfield == nil {
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info.TotalLength - t.bytesComplete()
	}
	return tr
}
