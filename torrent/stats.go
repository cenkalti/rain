package torrent

import (
	"math"
)

// Stats contains statistics about Torrent.
type Stats struct {
	// Status of the torrent.
	Status Status

	// Bytes that are downloaded and passed hash check.
	BytesComplete int64

	// BytesLeft is the number of bytes that is needed to complete all missing pieces.
	BytesIncomplete int64

	// BytesTotal is the number of total bytes of files in torrent.
	//
	// BytesTotal = BytesComplete + BytesIncomplete
	BytesTotal int64

	// BytesDownloaded is the number of bytes downloaded from swarm.
	// Because some pieces may be downloaded more than once, this number may be greater than BytesCompleted returns.
	// BytesDownloaded int64

	// BytesUploaded is the number of bytes uploaded to the swarm.
	// BytesUploaded   int64
}

func (t *Torrent) stats() Stats {
	stats := Stats{
		Status: t.status(),
	}
	if t.info != nil && t.bitfield != nil { // TODO split this if cond
		stats.BytesTotal = t.info.TotalLength
		// TODO this is wrong, pre-calculate complete and incomplete bytes
		stats.BytesComplete = int64(t.info.PieceLength) * int64(t.bitfield.Count())
		if t.bitfield.Test(t.bitfield.Len() - 1) {
			stats.BytesComplete -= int64(t.info.PieceLength)
			stats.BytesComplete += int64(t.pieces[t.bitfield.Len()-1].Length)
		}
		stats.BytesIncomplete = stats.BytesTotal - stats.BytesComplete
		// TODO calculate bytes downloaded
		// TODO calculate bytes uploaded
	} else {
		stats.BytesIncomplete = math.MaxUint32
		// TODO this is wrong, pre-calculate complete and incomplete bytes
	}
	return stats
}
