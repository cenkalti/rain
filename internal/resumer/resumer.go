// Package resumer contains an interface that is used by torrent package for resuming an existing download.
package resumer

// Resumer provides operations to save and load resume info for a Torrent.
type Resumer interface {
	WriteInfo([]byte) error
	WriteBitfield([]byte) error
	WriteStats(Stats) error
}

type Stats struct {
	BytesDownloaded int64
	BytesUploaded   int64
	BytesWasted     int64
	SecondsSeeded   uint32
}
