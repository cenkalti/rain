package rain

import (
	"testing"

	"github.com/cenkalti/log"
)

const (
	port        = 6881
	torrentFile = "/Users/cenk/random.txt.torrent"
	where       = "."
)

func init() {
	SetLogLevel(log.DEBUG)
}

func TestDownload(t *testing.T) {
	r, err := New(port)
	if err != nil {
		t.Fatal(err)
	}

	if err = r.Download(torrentFile, where); err != nil {
		t.Fatal(err)
	}
}
