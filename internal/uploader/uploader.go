package uploader

const (
	// Maximum simultaneous uploads.
	uploadSlotsPerTorrent = 4

	// Request pieces in blocks of this size.
	maxBlockSize = 16 * 1024
)

type Uploader struct {
}

func New() *Uploader {
	return &Uploader{}
}

func (u *Uploader) Run(stopC chan struct{}) {
}
