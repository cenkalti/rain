package rain_test

import (
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/cenkalti/log"
	"github.com/crosbymichael/tracker"
	"github.com/crosbymichael/tracker/server"

	"github.com/cenkalti/rain"
)

const (
	port1 = 6881
	port2 = 6882
)

var (
	trackerAddr    = ":5000"
	torrentFile    = filepath.Join("testfiles", "sample_torrent.torrent")
	torrentDataDir = "testfiles"
	torrentName    = "sample_torrent"
)

func init() {
	rain.SetLogLevel(log.DEBUG)
}

func TestDownload(t *testing.T) {
	c1 := rain.DefaultConfig
	c1.Port = port1

	c2 := rain.DefaultConfig
	c2.Port = port2

	seeder, err := rain.New(&c1)
	if err != nil {
		t.Fatal(err)
	}

	err = seeder.Listen()
	if err != nil {
		t.Fatal(err)
	}

	leecher, err := rain.New(&c2)
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

	t1, err := seeder.Add(torrentFile, torrentDataDir)
	if err != nil {
		t.Fatal(err)
	}
	seeder.Start(t1)

	// Wait for seeder to announce to tracker.
	time.Sleep(1 * time.Second)

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	t2, err := leecher.Add(torrentFile, where)
	if err != nil {
		t.Fatal(err)
	}
	leecher.Start(t2)

	select {
	case <-t2.Finished:
	case <-time.After(10 * time.Second):
		t.FailNow()
	}

	cmd := exec.Command("diff", "-rq",
		filepath.Join(torrentDataDir, torrentName),
		filepath.Join(where, torrentName))
	err = cmd.Run()
	if err != nil {
		t.Fatal(err)
	}

	err = os.RemoveAll(where)
	if err != nil {
		t.Fatal(err)
	}
}
