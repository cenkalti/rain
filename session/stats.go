package session

import (
	"math"
	"time"

	"github.com/cenkalti/rain/internal/addrlist"
)

// Stats contains statistics about Torrent.
type Stats struct {
	// Status of the torrent.
	Status TorrentStatus
	// Contains the error message if torrent is stopped unexpectedly.
	Error  error
	Pieces struct {
		// Number of pieces that are checked when torrent is in "Verifying" state.
		Checked uint32
		// Number of pieces that we are downloaded successfully and verivied by hash check.
		Have uint32
		// Number of pieces that need to be downloaded. Some of them may be being downloaded.
		// Pieces that are being downloaded may counted as missing until they are downloaded and passed hash check.
		Missing uint32
		// Number of unique pieces available on swarm.
		// If this number is less then the number of total pieces, the download may never finish.
		Available uint32
		// Number of total pieces in torrent.
		Total uint32
	}
	Bytes struct {
		// Bytes that are downloaded and passed hash check.
		Completed int64
		// The number of bytes that is needed to complete all missing pieces.
		Incomplete int64
		// The number of total bytes of files in torrent.  Total = Completed + Incomplete
		Total int64
		// Downloaded is the number of bytes downloaded from swarm.
		// Because some pieces may be downloaded more than once, this number may be greater than completed bytes.
		Downloaded int64
		// BytesUploaded is the number of bytes uploaded to the swarm.
		Uploaded int64
		// Bytes downloaded due to duplicate/non-requested pieces.
		Wasted int64
		// Bytes allocated on storage.
		Allocated int64
	}
	Peers struct {
		// Number of peers that are connected, handshaked and ready to send and receive messages.
		Total int
		// Number of peers that have connected to us.
		Incoming int
		// Number of peers that we have connected to.
		Outgoing int
	}
	Handshakes struct {
		// Number of peers that are not handshaked yet.
		Total int
		// Number of incoming peers in handshake state.
		Incoming int
		// Number of outgoing peers in handshake state.
		Outgoing int
	}
	Addresses struct {
		// Total number of peer addresses that are ready to be connected.
		Total int
		// Peers found via trackers.
		Tracker int
		// Peers found via DHT node.
		DHT int
		// Peers found via peer exchange.
		PEX int
	}
	Downloads struct {
		// Number of active piece downloads.
		Total int
		// Number of pieces that are being downloaded normally.
		Running int
		// Number of pieces that are being downloaded too slow.
		Snubbed int
		// Number of piece downloads in choked state.
		Choked int
	}
	MetadataDownloads struct {
		// Number of active metadata downloads.
		Total int
		// Number of peers that uploading too slow.
		Snubbed int
		// Number of peers that are being downloaded normally.
		Running int
	}
	// Name can change after metadata is downloaded.
	Name string
	// Is private torrent?
	Private bool
	// Length of a single piece.
	PieceLength uint32
	// Duration while the torrent is in Seeding status.
	SeededFor time.Duration
	// Speed is calculated as 1-minute moving average.
	Speed struct {
		// Downloaded bytes per second.
		Download uint
		// Uploaded bytes per second.
		Upload uint
	}
	// Time remaining to complete download. nil value means infinity.
	ETA *time.Duration
}

func (t *torrent) stats() Stats {
	t.updateSeedDuration()

	var s Stats
	s.Status = t.status()
	s.Error = t.lastError
	s.Addresses.Total = t.addrList.Len()
	s.Addresses.Tracker = t.addrList.LenSource(addrlist.Tracker)
	s.Addresses.DHT = t.addrList.LenSource(addrlist.DHT)
	s.Addresses.PEX = t.addrList.LenSource(addrlist.PEX)
	s.Handshakes.Incoming = len(t.incomingHandshakers)
	s.Handshakes.Outgoing = len(t.outgoingHandshakers)
	s.Handshakes.Total = len(t.incomingHandshakers) + len(t.outgoingHandshakers)
	s.Peers.Total = len(t.peers)
	s.Peers.Incoming = len(t.incomingPeers)
	s.Peers.Outgoing = len(t.outgoingPeers)
	s.MetadataDownloads.Total = len(t.infoDownloaders)
	s.MetadataDownloads.Snubbed = len(t.infoDownloadersSnubbed)
	s.MetadataDownloads.Running = len(t.infoDownloaders) - len(t.infoDownloadersSnubbed)
	s.Downloads.Total = len(t.pieceDownloaders)
	s.Downloads.Snubbed = len(t.pieceDownloadersSnubbed)
	s.Downloads.Choked = len(t.pieceDownloadersChoked)
	s.Downloads.Running = len(t.pieceDownloaders) - len(t.pieceDownloadersChoked) - len(t.pieceDownloadersSnubbed)
	s.Pieces.Available = t.avaliablePieceCount()
	s.Bytes.Downloaded = t.resumerStats.BytesDownloaded
	s.Bytes.Uploaded = t.resumerStats.BytesUploaded
	s.Bytes.Wasted = t.resumerStats.BytesWasted
	s.SeededFor = t.resumerStats.SeededFor
	s.Bytes.Allocated = t.bytesAllocated
	s.Pieces.Checked = t.checkedPieces
	s.Speed.Download = uint(t.downloadSpeed.Rate())
	s.Speed.Upload = uint(t.uploadSpeed.Rate())

	if t.info != nil {
		s.Bytes.Total = t.info.TotalLength
		s.Bytes.Completed = t.bytesComplete()
		s.Bytes.Incomplete = s.Bytes.Total - s.Bytes.Completed

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
	bps := int64(s.Speed.Download)
	if bps != 0 {
		eta := time.Duration(s.Bytes.Incomplete/bps) * time.Second
		s.ETA = &eta
	}
	return s
}

func (t *torrent) avaliablePieceCount() uint32 {
	if t.piecePicker == nil {
		return 0
	}
	return t.piecePicker.Available()
}

func (t *torrent) bytesComplete() int64 {
	if t.bitfield == nil || len(t.pieces) == 0 {
		return 0
	}
	n := int64(t.info.PieceLength) * int64(t.bitfield.Count())
	if t.bitfield.Test(t.bitfield.Len() - 1) {
		n -= int64(t.info.PieceLength)
		n += int64(t.pieces[t.bitfield.Len()-1].Length)
	}
	return n
}

func (t *torrent) getTrackers() []Tracker {
	var trackers []Tracker
	for _, an := range t.announcers {
		st := an.Stats()
		t := Tracker{
			URL:      an.Tracker.URL(),
			Status:   TrackerStatus(st.Status),
			Seeders:  st.Seeders,
			Leechers: st.Leechers,
			Error:    st.Error,
		}
		trackers = append(trackers, t)
	}
	return trackers
}

func (t *torrent) getPeers() []Peer {
	var peers []Peer
	for pe := range t.peers {
		p := Peer{
			Addr: pe.Addr(),
		}
		peers = append(peers, p)
	}
	return peers
}

func (t *torrent) updateSeedDuration() {
	if t.status() != Seeding {
		t.seedDurationUpdatedAt = time.Time{}
		return
	}
	if t.seedDurationUpdatedAt.IsZero() {
		t.seedDurationUpdatedAt = time.Now()
		return
	}
	now := time.Now()
	t.resumerStats.SeededFor += now.Sub(t.seedDurationUpdatedAt)
	t.seedDurationUpdatedAt = now
}
