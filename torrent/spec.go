package torrent

import (
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
)

// downloadSpec contains parameters for Torrent constructor.
type downloadSpec struct {
	// Identifies the torrent being downloaded.
	infoHash [20]byte
	// List of addresses to announce this torrent.
	trackers []string
	// Name of the torrent.
	name string
	// Storage implementation to save the files in torrent.
	storage storage.Storage
	// TCP Port to listen for peer connections.
	port int
	// Optional DB implementation to save resume state of the torrent.
	resume resume.DB
	// Contains info about files in torrent. This can be nil at start for magnet downloads.
	info *metainfo.Info
	// Bitfield for pieces we have. It is created after we got info.
	bitfield *bitfield.Bitfield
}
