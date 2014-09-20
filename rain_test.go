package rain_test

import (
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/cenkalti/log"
	"github.com/crosbymichael/tracker"
	"github.com/crosbymichael/tracker/server"

	"github.com/cenkalti/rain"
)

const (
	port1       = 6881
	port2       = 6882
	trackerAddr = ":5000"
	torrentFile = "/Users/cenk/torrent_test/sample_torrent.torrent"
	torrentData = "/Users/cenk/torrent_test/sample_torrent"
)

func init() {
	rain.SetLogLevel(log.DEBUG)
}

func TestDownload(t *testing.T) {
	c1 := rain.DefaultConfig
	c1.Port = port1

	c2 := rain.DefaultConfig
	c2.Port = port1

	r1, err := rain.New(&c1) // Seeder
	if err != nil {
		t.Fatal(err)
	}

	err = r1.Listen()
	if err != nil {
		t.Fatal(err)
	}

	r2, err := rain.New(&c2) // Leecher
	if err != nil {
		t.Fatal(err)
	}

	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	registry := tracker.NewInMemoryRegistry()
	s := server.New(120, 30, registry, logger)
	go func() {
		if err := http.ListenAndServe(trackerAddr, s); err != nil {
			t.Fatal(err)
		}
	}()

	go r1.Run(torrentFile, "")

	// Wait for r1 to announce to tracker.
	time.Sleep(2 * time.Second)

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	if err = r2.Download(torrentFile, where); err != nil {
		t.Fatal(err)
	}
}
