package downloader

import (
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/resume"
	"github.com/cenkalti/rain/storage"
)

// Spec contains parameters for Download constructor.
type Spec struct {
	InfoHash [20]byte
	Storage  storage.Storage
	Resume   resume.DB
	Info     *metainfo.Info
	Bitfield *bitfield.Bitfield
}
