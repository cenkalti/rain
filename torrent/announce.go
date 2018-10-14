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
	}
	if t.bitfield == nil {
		tr.BytesLeft = math.MaxUint32
	} else {
		// TODO this is wrong, pre-calculate complete and incomplete bytes
		tr.BytesLeft = t.info.TotalLength - int64(t.info.PieceLength)*int64(t.bitfield.Count())
	}
	return tr
}
