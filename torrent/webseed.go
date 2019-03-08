package torrent

import (
	"github.com/cenkalti/rain/internal/webseeddownloader"
)

func (t *torrent) setWebseedDownloader() {
	if len(t.webseedSources) > 0 {
		t.webseedDownloader = webseeddownloader.New(t.webseedClient, t.webseedSources, t.bitfield)
	}
}
