package webseedsource

import (
	"time"

	"github.com/cenkalti/rain/internal/urldownloader"
	"github.com/rcrowley/go-metrics"
)

// WebseedSource is a URL for downloading torrent data from web sources.
type WebseedSource struct {
	URL           string
	Disabled      bool
	Downloader    *urldownloader.URLDownloader
	LastError     error
	DisabledAt    time.Time
	DownloadSpeed metrics.Meter
}

// NewList returns a new WebseedSource list.
func NewList(sources []string) []*WebseedSource {
	l := make([]*WebseedSource, len(sources))
	for i := range sources {
		l[i] = &WebseedSource{
			URL:           sources[i],
			DownloadSpeed: metrics.NilMeter{},
		}
	}
	return l
}

// Downloading returns true if data is being downloaded from this source.
func (s *WebseedSource) Downloading() bool {
	return s.Downloader != nil
}

// Remaining returns the number of pieces that is going to be downloaded by this source.
// If there is a piece currently downloading, it is not counted.
func (s *WebseedSource) Remaining() uint32 {
	if s.Downloader == nil {
		return 0
	}
	return s.Downloader.End - s.Downloader.ReadCurrent() - 1
}
