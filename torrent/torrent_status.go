package torrent

// Status of a Torrent
type Status int

const (
	// Stopped indicates that the torrent is not running.
	// No peers are connected and files are not open.
	Stopped Status = iota
	// DownloadingMetadata indicates that the torrent is in the process of downloadin metadata.
	// When torrent is added via magnet link, torrent has no metadata and it needs to be downloaded from peers before starting to download files.
	DownloadingMetadata
	// Allocating indicates that the torrent is in the process of creating/opening files on the disk.
	Allocating
	// Verifying indicates that the torrent has already some files on disk and it is checking the validity of pieces by comparing hashes.
	Verifying
	// Downloading the torrent's files from peers.
	Downloading
	// Seeding the torrent. All pieces/files are downloaded.
	Seeding
	// Stopping the torrent. This is the status after Stop() is called. All peers are disconnected and files are closed. A stop event sent to all trackers. After trackers responded the torrent switches into Stopped state.
	Stopping
)

func (s Status) String() string {
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
