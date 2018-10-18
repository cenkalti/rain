package torrent_test

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/storage/filestorage"
	"github.com/cenkalti/rain/torrent"
	"github.com/chihaya/chihaya/frontend/http"
	"github.com/chihaya/chihaya/frontend/udp"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/storage"
	_ "github.com/chihaya/chihaya/storage/memory"
)

var (
	trackerAddr    = ":5000"
	torrentFile    = filepath.Join("testdata", "10mb.torrent")
	torrentFileUDP = filepath.Join("testdata", "10mb_udp.torrent")
	torrentDataDir = "testdata"
	torrentName    = "10mb"
	timeout        = 10 * time.Second
)

func init() {
	logger.Handler.SetLevel(log.DEBUG)
}

func newFileStorage(t *testing.T, dir string) *filestorage.FileStorage {
	sto, err := filestorage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sto
}

func TestDownloadTorrent(t *testing.T) {
	defer startHTTPTracker(t)()

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := torrent.New(f, 6881, newFileStorage(t, torrentDataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer t1.Close()

	f.Seek(0, io.SeekStart)
	t2, err := torrent.New(f, 6882, newFileStorage(t, where))
	if err != nil {
		t.Fatal(err)
	}
	defer t2.Close()

	t1.Start()
	time.Sleep(100 * time.Millisecond)
	t2.Start()

	select {
	case <-t2.NotifyComplete():
	case err = <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		panic("download did not finish")
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

func trackerLogic(t *testing.T) *middleware.Logic {
	responseConfig := middleware.Config{
		AnnounceInterval:    time.Minute,
		MaxNumWant:          200,
		DefaultNumWant:      50,
		MaxScrapeInfoHashes: 400,
	}
	ps, err := storage.NewPeerStore("memory", map[string]interface{}{
		"gc_interval":                   3 * time.Minute,
		"peer_lifetime":                 31 * time.Minute,
		"shard_count":                   1024,
		"prometheus_reporting_interval": time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return middleware.NewLogic(responseConfig, ps, nil, nil)
}

func startHTTPTracker(t *testing.T) (stop func()) {
	lgc := trackerLogic(t)
	httpfe, err := http.NewFrontend(lgc, http.Config{
		Addr:         "127.0.0.1:5000",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		errC := httpfe.Stop()
		err := <-errC
		if err != nil {
			t.Fatal(err)
		}
	}
}

func startUDPTracker(t *testing.T, port int) {
	lgc := trackerLogic(t)
	_, err := udp.NewFrontend(lgc, udp.Config{
		Addr:            "127.0.0.1:" + strconv.Itoa(port),
		MaxClockSkew:    time.Minute,
		AllowIPSpoofing: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDownloadMagnet(t *testing.T) {
	defer startHTTPTracker(t)()

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := torrent.New(f, 6881, newFileStorage(t, torrentDataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer t1.Close()

	f.Seek(0, io.SeekStart)
	magnetLink := "magnet:?xt=urn:btih:0a8e2e8c9371a91e9047ed189ceffbc460803262&dn=10mb&tr=http%3A%2F%2F127.0.0.1%3A5000%2Fannounce"
	t2, err := torrent.NewMagnet(magnetLink, 6882, newFileStorage(t, where))
	if err != nil {
		t.Fatal(err)
	}
	defer t2.Close()

	t1.Start()
	time.Sleep(100 * time.Millisecond)
	t2.Start()

	select {
	case <-t2.NotifyComplete():
	case err = <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		panic("download did not finish")
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

func TestDownloadTorrentUDP(t *testing.T) {
	startUDPTracker(t, 5000) // TODO this does not get closed

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(torrentFileUDP)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := torrent.New(f, 6881, newFileStorage(t, torrentDataDir))
	if err != nil {
		t.Fatal(err)
	}
	defer t1.Close()

	f.Seek(0, io.SeekStart)
	t2, err := torrent.New(f, 6882, newFileStorage(t, where))
	if err != nil {
		t.Fatal(err)
	}
	defer t2.Close()

	t1.Start()
	time.Sleep(100 * time.Millisecond)
	t2.Start()

	select {
	case <-t2.NotifyComplete():
	case err = <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		panic("download did not finish")
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
