// Package resumer contains an interface that is used by torrent package for resuming an existing download.
package resumer

// Stats of a torrent.
type Stats struct {
	BytesDownloaded int64
	BytesUploaded   int64
	BytesWasted     int64
	SeededFor       int64 // time.Duration
}
