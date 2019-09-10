package webseedsource

import (
	"time"

	"github.com/cenkalti/rain/internal/urldownloader"
	"github.com/rcrowley/go-metrics"
)

type WebseedSource struct {
	URL           string
	Disabled      bool
	Downloader    *urldownloader.URLDownloader
	LastError     error
	DisabledAt    time.Time
	DownloadSpeed metrics.Meter
}

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
