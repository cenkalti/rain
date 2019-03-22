package webseedsource

import (
	"github.com/cenkalti/rain/internal/urldownloader"
)

type WebseedSource struct {
	URL        string
	Disabled   bool
	Downloader *urldownloader.URLDownloader
}

func New(source string) *WebseedSource {
	return &WebseedSource{URL: source}
}

func NewList(sources []string) []*WebseedSource {
	l := make([]*WebseedSource, len(sources))
	for i := range sources {
		l[i] = New(sources[i])
	}
	return l
}

func (s *WebseedSource) Downloading() bool {
	return s.Downloader != nil
}
