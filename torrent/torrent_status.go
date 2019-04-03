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
	switch {
	case t.errC == nil:
		return Stopped
	case t.stoppedEventAnnouncer != nil:
		return Stopping
	case t.allocator != nil:
		return Allocating
	case t.verifier != nil:
		return Verifying
	case t.completed:
		return Seeding
	case t.info == nil:
		return DownloadingMetadata
	default:
		return Downloading
	}
}
