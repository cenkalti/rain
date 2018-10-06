package torrent

import (
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
)

// type Spec struct {
// 	InfoHash []byte
// 	Trackers []string
// 	Name string
// 	InfoBytes []byte
// }

// downloadSpec contains parameters for Download constructor.
type downloadSpec struct {
	InfoHash [20]byte
	Trackers []string
	Name     string
	Storage  storage.Storage
	Port     int
	Resume   resume.DB
	Info     *metainfo.Info
	Bitfield *bitfield.Bitfield
}
