package torrent

import (
	"math"
)

// Stats contains statistics about Torrent.
type Stats struct {
	// Status of the torrent.
	Status Status

	Pieces struct {
		Have    uint32
		Missing uint32
		Total   uint32
	}

	Bytes struct {
		// Bytes that are downloaded and passed hash check.
		Complete int64

		// The number of bytes that is needed to complete all missing pieces.
		Incomplete int64

		// The number of total bytes of files in torrent.
		//
		// Total = Complete + Incomplete
		Total int64

		// Downloaded is the number of bytes downloaded from swarm.
		// Because some pieces may be downloaded more than once, this number may be greater than BytesCompleted returns.
		// TODO put into resume
		Downloaded int64

		Wasted int64

		// BytesUploaded is the number of bytes uploaded to the swarm.
		// TODO BytesUploaded   int64
	}

	Peers struct {
		Connected struct {
			// Number of peers that are connected, handshaked and ready to send and receive messages.
			// ConnectedPeers = IncomingPeers + OutgoingPeers
			Total int

			// Number of peers that have connected to us.
			Incoming int

			// Number of peers that we have connected to.
			Outgoing int
		}

		Handshake struct {
			// Number of peers that are not handshaked yet.
			Total int

			// Number of incoming peers in handshake state.
			Incoming int

			// Number of outgoing peers in handshake state.
			Outgoing int
		}

		// Number of peer addresses that are ready to be connected.
		Ready int
	}

	Downloads struct {
		Piece struct {
			// Number of active piece downloads.
			Total int

			// Number of pieces that are being downloaded normally.
			Running int

			// Number of pieces that are being downloaded too slow.
			Snubbed int

			// Number of piece downloads in choked state.
			Choked int
		}

		Metadata struct {
			// Number of active metadata downloads.
			Total int

			// Number of peers that uploading too slow.
			Snubbed int

			// Number of peers that are being downloaded normally.
			Running int
		}
	}
}

func (t *Torrent) stats() Stats {
	var stats Stats
	stats.Status = t.status()
	stats.Peers.Ready = t.addrList.Len()
	stats.Peers.Handshake.Incoming = len(t.incomingHandshakers)
	stats.Peers.Handshake.Outgoing = len(t.outgoingHandshakers)
	stats.Peers.Handshake.Total = len(t.incomingHandshakers) + len(t.outgoingHandshakers)
	stats.Peers.Connected.Total = len(t.peers)
	stats.Peers.Connected.Incoming = len(t.incomingPeers)
	stats.Peers.Connected.Outgoing = len(t.outgoingPeers)
	stats.Downloads.Metadata.Total = len(t.infoDownloaders)
	stats.Downloads.Metadata.Snubbed = len(t.infoDownloadersSnubbed)
	stats.Downloads.Metadata.Running = len(t.infoDownloaders) - len(t.infoDownloadersSnubbed)
	stats.Downloads.Piece.Total = len(t.pieceDownloaders)
	stats.Downloads.Piece.Snubbed = len(t.pieceDownloadersSnubbed)
	stats.Downloads.Piece.Choked = len(t.pieceDownloadersChoked)
	stats.Downloads.Piece.Running = len(t.pieceDownloaders) - len(t.pieceDownloadersChoked) - len(t.pieceDownloadersSnubbed)

	if t.info != nil {
		stats.Bytes.Total = t.info.TotalLength
		stats.Bytes.Complete = t.bytesComplete()
		stats.Bytes.Incomplete = stats.Bytes.Total - stats.Bytes.Complete
	} else {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		stats.Bytes.Incomplete = math.MaxUint32
	}
	if t.bitfield != nil {
		stats.Pieces.Total = t.bitfield.Len()
		stats.Pieces.Have = t.bitfield.Count()
		stats.Pieces.Missing = stats.Pieces.Total - stats.Pieces.Have
	}
	stats.Bytes.Downloaded = t.bytesDownloaded
	stats.Bytes.Wasted = t.bytesWasted
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
