package session

type TorrentStatus int

const (
	Stopped TorrentStatus = iota
	DownloadingMetadata
	Allocating
	Verifying
	Downloading
	Seeding
	Stopping
)

func torrentStatusToString(s TorrentStatus) string {
	m := map[TorrentStatus]string{
		Stopped:             "Stopped",
		DownloadingMetadata: "Downloading Metadata",
		Allocating:          "Allocating",
		Verifying:           "Verifying",
		Downloading:         "Downloading",
		Seeding:             "Seeding",
		Stopping:            "Stopping",
	}
	return m[s]
}

func (t *torrent) status() TorrentStatus {
	if t.errC == nil {
		return Stopped
	}
	if t.stoppedEventAnnouncer != nil {
		return Stopping
	}
	if t.allocator != nil {
		return Allocating
	}
	if t.verifier != nil {
		return Verifying
	}
	if t.completed {
		return Seeding
	}
	if t.info == nil {
		return DownloadingMetadata
	}
	return Downloading
}
