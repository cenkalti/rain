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
	// TODO BytesDownloaded int64

	// BytesUploaded is the number of bytes uploaded to the swarm.
	// TODO BytesUploaded   int64

	// Number of peers that are connected, handshaked and ready to send and receive messages.
	// ConnectedPeers = IncomingPeers + OutgoingPeers
	ConnectedPeers int

	// Number of peers that have connected to us.
	IncomingPeers int

	// Number of peers that we have connected to.
	OutgoingPeers int

	// Number of active piece downloads.
	ActiveDownloads int

	// Number of pieces that are being downloaded normally.
	RunningDownloads int

	// Number of peers that uploading too slow.
	SnubbedDownloads int

	// Number of active piece downloads in choked state.
	ChokedDownloads int

	// Number of active metadata downloads.
	ActiveMetadataDownloads int

	// Number of peer addresses that are ready to be connected.
	ReadyPeerAddresses int

	// Number of incoming peers in handshake state.
	IncomingHandshakes int

	// Number of outgoing peers in handshake state.
	OutgoingHandshakes int
}

func (t *Torrent) stats() Stats {
	stats := Stats{
		Status:                  t.status(),
		ConnectedPeers:          len(t.peers),
		IncomingPeers:           len(t.incomingPeers),
		OutgoingPeers:           len(t.outgoingPeers),
		ActiveDownloads:         len(t.pieceDownloaders),
		ActiveMetadataDownloads: len(t.infoDownloaders),
		RunningDownloads:        len(t.pieceDownloaders) - len(t.chokedDownloaders) - len(t.snubbedDownloaders),
		SnubbedDownloads:        len(t.snubbedDownloaders),
		ChokedDownloads:         len(t.chokedDownloaders),
		ReadyPeerAddresses:      t.addrList.Len(),
		IncomingHandshakes:      len(t.incomingHandshakers),
		OutgoingHandshakes:      len(t.outgoingHandshakers),
	}
	if t.info != nil {
		stats.BytesTotal = t.info.TotalLength
		stats.BytesComplete = t.bytesComplete()
		stats.BytesIncomplete = stats.BytesTotal - stats.BytesComplete
	} else {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		stats.BytesIncomplete = math.MaxUint32
	}
	return stats
}

func (t *Torrent) bytesComplete() int64 {
	if t.bitfield == nil {
		return 0
	}
	n := int64(t.info.PieceLength) * int64(t.bitfield.Count())
	if t.bitfield.Test(t.bitfield.Len() - 1) {
		n -= int64(t.info.PieceLength)
		n += int64(t.pieces[t.bitfield.Len()-1].Length)
	}
	return n
}
