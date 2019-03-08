package webseeddownloader

import (
	"net/http"

	"github.com/cenkalti/rain/internal/bitfield"
)

type WebseedDownloader struct {
	client      *http.Client
	bitfield    *bitfield.Bitfield
	downloaders []*urlDownloader
}

func New(client *http.Client, sources []string, bf *bitfield.Bitfield) *WebseedDownloader {
	d := &WebseedDownloader{
		client:   client,
		bitfield: bf,
	}
	for _, s := range sources {
		d.downloaders = append(d.downloaders, &urlDownloader{
			source: s,
		})
	}
	return d
}

func (d *WebseedDownloader) Start() {
}
