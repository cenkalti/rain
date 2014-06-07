package rain

import "github.com/cenkalti/hub"

// States
const (
	DownloadStopped = iota
	DownloadRunning
	DownloadSeeding
)

// Events
const (
	DownloadFinished = iota
)

// Download represents an active download in the program.
type Download struct {
	TorrentFile *TorrentFile
	Events      hub.Hub
	// Stats
	Downloaded int64
	Uploaded   int64
	// Left       int64
}

func (d *Download) Left() int64 {
	return d.TorrentFile.TotalLength - d.Downloaded
}

func NewDownload(t *TorrentFile) *Download {
	return &Download{TorrentFile: t}
}
