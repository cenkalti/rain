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
	downloadSpeed metrics.EWMA
}

func NewList(sources []string) []*WebseedSource {
	l := make([]*WebseedSource, len(sources))
	for i := range sources {
		l[i] = &WebseedSource{
			URL:           sources[i],
			downloadSpeed: metrics.NewEWMA1(),
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
	return s.Downloader.End - s.Downloader.Current - 1
}

func (s *WebseedSource) DownloadSpeed() uint {
	return uint(s.downloadSpeed.Rate())
}

func (s *WebseedSource) TickSpeed() {
	s.downloadSpeed.Tick()
}

func (s *WebseedSource) UpdateSpeed(length int) {
	s.downloadSpeed.Update(int64(length))
}

func (s *WebseedSource) ResetSpeed() {
	s.downloadSpeed = metrics.NewEWMA1()
}
