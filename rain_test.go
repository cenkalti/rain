package rain_test

import (
	"io/ioutil"
	"net"
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
	torrentFile = "testfiles/sample_torrent.torrent"
	torrentData = "testfiles"
)

func init() {
	rain.SetLogLevel(log.DEBUG)
}

func TestDownload(t *testing.T) {
	c1 := rain.DefaultConfig
	c1.Port = port1

	c2 := rain.DefaultConfig
	c2.Port = port2

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

	// Start tracker
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	registry := tracker.NewInMemoryRegistry()
	s := server.New(120, 30, registry, logger)
	l, err := net.Listen("tcp", trackerAddr)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := http.Serve(l, s); err != nil {
			t.Fatal(err)
		}
	}()

	t1, err := r1.Add(torrentFile, torrentData)
	if err != nil {
		t.Fatal(err)
	}
	r1.Start(t1)

	// Wait for r1 to announce to tracker.
	time.Sleep(2 * time.Second)

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	t2, err := r2.Add(torrentFile, where)
	if err != nil {
		t.Fatal(err)
	}
	r2.Start(t2)
	<-t2.Finished
}
