package torrent

import (
	"time"

	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peersource"
	"github.com/cenkalti/rain/internal/stringutil"
)

// Stats contains statistics about Torrent.
type Stats struct {
	// Info hash of torrent.
	InfoHash InfoHash
	// Listening port number.
	Port int
	// Status of the torrent.
	Status Status
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
	// Number of files.
	FileCount int
	// Length of a single piece.
	PieceLength uint32
	// Duration while the torrent is in Seeding status.
	SeededFor time.Duration
	// Speed is calculated as 1-minute moving average.
	Speed struct {
		// Downloaded bytes per second.
		Download int
		// Uploaded bytes per second.
		Upload int
	}
	// Time remaining to complete download. nil value means infinity.
	ETA *time.Duration
}

func (t *torrent) stats() Stats {
	t.updateSeedDuration(time.Now())

	var s Stats
	s.InfoHash = t.infoHash
	s.Port = t.port
	s.Status = t.status()
	s.Error = t.lastError
	s.Addresses.Total = t.addrList.Len()
	s.Addresses.Tracker = t.addrList.LenSource(peersource.Tracker)
	s.Addresses.DHT = t.addrList.LenSource(peersource.DHT)
	s.Addresses.PEX = t.addrList.LenSource(peersource.PEX)
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
	s.Bytes.Downloaded = t.bytesDownloaded.Count()
	s.Bytes.Uploaded = t.bytesUploaded.Count()
	s.Bytes.Wasted = t.bytesWasted.Count()
	s.SeededFor = time.Duration(t.seededFor.Count())
	s.Bytes.Allocated = t.bytesAllocated
	s.Pieces.Checked = t.checkedPieces
	s.Speed.Download = int(t.downloadSpeed.Rate1())
	s.Speed.Upload = int(t.uploadSpeed.Rate1())

	if t.info != nil {
		s.Bytes.Total = t.info.Length
		s.Bytes.Completed = t.bytesComplete()
		s.Bytes.Incomplete = s.Bytes.Total - s.Bytes.Completed

		s.Name = t.info.Name
		s.Private = t.info.Private
		s.FileCount = len(t.info.Files)
		s.PieceLength = t.info.PieceLength
		s.Pieces.Total = t.info.NumPieces
	} else {
		s.Name = t.name
	}
	s.Name = stringutil.Printable(s.Name)
	if t.bitfield != nil {
		s.Pieces.Have = t.bitfield.Count()
		s.Pieces.Missing = s.Pieces.Total - s.Pieces.Have
	}
	if s.Status == Downloading {
		bps := int64(s.Speed.Download)
		if bps != 0 {
			eta := time.Duration(s.Bytes.Incomplete/bps) * time.Second
			switch {
			case eta > 8*time.Hour:
				eta = eta.Round(time.Hour)
			case eta > 4*time.Hour:
				eta = eta.Round(30 * time.Minute)
			case eta > 2*time.Hour:
				eta = eta.Round(15 * time.Minute)
			case eta > time.Hour:
				eta = eta.Round(5 * time.Minute)
			case eta > 30*time.Minute:
				eta = eta.Round(1 * time.Minute)
			case eta > 15*time.Minute:
				eta = eta.Round(30 * time.Second)
			case eta > 5*time.Minute:
				eta = eta.Round(15 * time.Second)
			case eta > time.Minute:
				eta = eta.Round(5 * time.Second)
			}
			s.ETA = &eta
		}
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
	trackers := make([]Tracker, len(t.announcers))
	for i, an := range t.announcers {
		st := an.Stats()
		trackers[i] = Tracker{
			URL:          an.Tracker.URL(),
			Status:       TrackerStatus(st.Status),
			Seeders:      st.Seeders,
			Leechers:     st.Leechers,
			Warning:      st.Warning,
			LastAnnounce: st.LastAnnounce,
			NextAnnounce: st.NextAnnounce,
		}
		if st.Error != nil {
			trackers[i].Error = &AnnounceError{st.Error}
		}
	}
	return trackers
}

func (t *torrent) getPeers() []Peer {
	peers := make([]Peer, 0, len(t.peers))
	for pe := range t.peers {
		var source PeerSource
		switch pe.Source {
		case peersource.Tracker:
			source = SourceTracker
		case peersource.DHT:
			source = SourceDHT
		case peersource.PEX:
			source = SourcePEX
		case peersource.Incoming:
			source = SourceIncoming
		case peersource.Manual:
			source = SourceManual
		default:
			t.crash("unhandled peer source")
		}
		p := Peer{
			ID:                 pe.ID,
			Client:             pe.Client(),
			Addr:               pe.Addr(),
			ConnectedAt:        pe.ConnectedAt,
			Downloading:        pe.Downloading,
			ClientInterested:   pe.ClientInterested,
			ClientChoking:      pe.ClientChoking,
			PeerInterested:     pe.PeerInterested,
			PeerChoking:        pe.PeerChoking,
			OptimisticUnchoked: pe.OptimisticUnchoked,
			Snubbed:            pe.Snubbed,
			EncryptedHandshake: pe.EncryptionCipher != 0,
			EncryptedStream:    pe.EncryptionCipher == mse.RC4,
			Source:             source,
			DownloadSpeed:      pe.DownloadSpeed(),
			UploadSpeed:        pe.UploadSpeed(),
		}
		peers = append(peers, p)
	}
	return peers
}

func (t *torrent) getWebseeds() []Webseed {
	webseeds := make([]Webseed, 0, len(t.webseedSources))
	for _, src := range t.webseedSources {
		ws := Webseed{
			URL:           src.URL,
			Error:         src.LastError,
			DownloadSpeed: int(src.DownloadSpeed.Rate1()),
		}
		webseeds = append(webseeds, ws)
	}
	return webseeds
}

func (t *torrent) updateSeedDuration(now time.Time) {
	if t.status() != Seeding {
		t.seedDurationUpdatedAt = time.Time{}
		return
	}
	if t.seedDurationUpdatedAt.IsZero() {
		t.seedDurationUpdatedAt = now
		return
	}
	t.seededFor.Inc(int64(now.Sub(t.seedDurationUpdatedAt)))
	t.seedDurationUpdatedAt = now
}
