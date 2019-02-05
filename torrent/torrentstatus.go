package torrent

type Status int

const (
	Stopped Status = iota
	DownloadingMetadata
	Allocating
	Verifying
	Downloading
	Seeding
	Stopping
)

func torrentStatusToString(s Status) string {
	m := map[Status]string{
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

func (t *torrent) status() Status {
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
