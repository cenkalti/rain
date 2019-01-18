package torrent

type Status string

const (
	Stopped             Status = "Stopped"
	DownloadingMetadata        = "Downloading Metadata"
	Allocating                 = "Allocating"
	Verifying                  = "Verifying"
	Downloading                = "Downloading"
	Seeding                    = "Seeding"
	Stopping                   = "Stopping"
)

func (t *Torrent) status() Status {
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
