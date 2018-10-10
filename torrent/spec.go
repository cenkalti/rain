package torrent

import (
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
)

// downloadSpec contains parameters for Download constructor.
type downloadSpec struct {
	infoHash [20]byte
	trackers []string
	name     string
	storage  storage.Storage
	port     int
	resume   resume.DB
	info     *metainfo.Info
	bitfield *bitfield.Bitfield
}
