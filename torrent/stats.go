package torrent

import "math"

// Stats contains statistics about Torrent.
type Stats struct {
	// Status of the torrent.
	Status Status
	// Contains the error if torrent is stopped unexpectedly.
	Error  error
	Pieces struct {
		Have      uint32
		Missing   uint32
		Available uint32
		Total     uint32
	}
	Bytes struct {
		// Bytes that are downloaded and passed hash check.
		Complete int64
		// The number of bytes that is needed to complete all missing pieces.
		Incomplete int64
		// The number of total bytes of files in torrent.  Total = Complete + Incomplete
		Total int64
		// Downloaded is the number of bytes downloaded from swarm.
		// Because some pieces may be downloaded more than once, this number may be greater than BytesCompleted returns.
		// TODO put into resume
		Downloaded int64
		// Protocol messages are not included, only piece data is counted.
		Uploaded int64
		Wasted   int64
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
	// Name can change after metadata is downloaded.
	Name string
	// Is private torrent?
	Private bool
	// Length of a single piece.
	PieceLength uint32
}

func (t *Torrent) stats() Stats {
	var s Stats
	s.Status = t.status()
	s.Error = t.lastError
	s.Peers.Ready = t.addrList.Len()
	s.Peers.Handshake.Incoming = len(t.incomingHandshakers)
	s.Peers.Handshake.Outgoing = len(t.outgoingHandshakers)
	s.Peers.Handshake.Total = len(t.incomingHandshakers) + len(t.outgoingHandshakers)
	s.Peers.Connected.Total = len(t.peers)
	s.Peers.Connected.Incoming = len(t.incomingPeers)
	s.Peers.Connected.Outgoing = len(t.outgoingPeers)
	s.Downloads.Metadata.Total = len(t.infoDownloaders)
	s.Downloads.Metadata.Snubbed = len(t.infoDownloadersSnubbed)
	s.Downloads.Metadata.Running = len(t.infoDownloaders) - len(t.infoDownloadersSnubbed)
	s.Downloads.Piece.Total = len(t.pieceDownloaders)
	s.Downloads.Piece.Snubbed = len(t.pieceDownloadersSnubbed)
	s.Downloads.Piece.Choked = len(t.pieceDownloadersChoked)
	s.Downloads.Piece.Running = len(t.pieceDownloaders) - len(t.pieceDownloadersChoked) - len(t.pieceDownloadersSnubbed)
	s.Pieces.Available = t.avaliablePieceCount()
	s.Bytes.Downloaded = t.bytesDownloaded
	s.Bytes.Uploaded = t.bytesUploaded
	s.Bytes.Wasted = t.bytesWasted

	if t.info != nil {
		s.Bytes.Total = t.info.TotalLength
		s.Bytes.Complete = t.bytesComplete()
		s.Bytes.Incomplete = s.Bytes.Total - s.Bytes.Complete

		s.Name = t.info.Name
		s.Private = (t.info.Private == 1)
		s.PieceLength = t.info.PieceLength
	} else {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		s.Bytes.Incomplete = math.MaxUint32

		s.Name = t.name
	}
	if t.bitfield != nil {
		s.Pieces.Total = t.bitfield.Len()
		s.Pieces.Have = t.bitfield.Count()
		s.Pieces.Missing = s.Pieces.Total - s.Pieces.Have
	}
	return s
}

func (t *Torrent) avaliablePieceCount() uint32 {
	var n uint32
	for _, pi := range t.pieces {
		if len(pi.HavingPeers) > 0 {
			n++
		}
	}
	return n
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
