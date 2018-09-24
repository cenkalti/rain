package torrent_test

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cenkalti/log"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/resume/torrentresume"
	"github.com/cenkalti/rain/storage/filestorage"
	"github.com/cenkalti/rain/torrent"
	"github.com/chihaya/chihaya/config"
	"github.com/chihaya/chihaya/http"
	"github.com/chihaya/chihaya/tracker"
)

var (
	trackerAddr    = ":5000"
	torrentFile    = filepath.Join("testdata", "10mb.torrent")
	torrentDataDir = "testdata"
	torrentName    = "10mb"
	timeout        = 10 * time.Second
)

func init() {
	logger.SetLogLevel(log.DEBUG)
}

func newResumeFile(t *testing.T) *torrentresume.TorrentResume {
	resumeFile, err := ioutil.TempFile("", "rain-test-")
	if err != nil {
		t.Fatal(err)
	}
	resumePath := resumeFile.Name()
	res, err := torrentresume.New(resumePath)
	if err != nil {
		t.Fatal(err)
	}
	return res
	// TODO remove tempfile
}

func TestDownloadTorrent(t *testing.T) {
	defer startTracker(t)()

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	sto1 := filestorage.New(torrentDataDir)
	res1 := newResumeFile(t)
	defer res1.Close()

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := torrent.New(f, 6881, sto1, res1)
	if err != nil {
		t.Fatal(err)
	}
	defer t1.Close()

	// Wait for seeder to announce to tracker.
	time.Sleep(100 * time.Millisecond)

	sto2 := filestorage.New(where)
	res2 := newResumeFile(t)
	defer res2.Close()

	f.Seek(0, io.SeekStart)
	t2, err := torrent.New(f, 6882, sto2, res2)
	if err != nil {
		t.Fatal(err)
	}
	defer t2.Close()

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

func startTracker(t *testing.T) (stop func()) {
	var cfg = new(config.Config)
	*cfg = config.DefaultConfig
	cfg.HTTPConfig.ListenAddr = "localhost:5000"
	trk, err := tracker.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := http.NewServer(cfg, trk)
	go srv.Serve()
	return func() {
		srv.Stop()
		err := trk.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadMagnet(t *testing.T) {
	defer startTracker(t)()

	where, err := ioutil.TempDir("", "rain-")
	if err != nil {
		t.Fatal(err)
	}

	sto1 := filestorage.New(torrentDataDir)
	res1 := newResumeFile(t)
	defer res1.Close()

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := torrent.New(f, 6881, sto1, res1)
	if err != nil {
		t.Fatal(err)
	}
	defer t1.Close()

	// Wait for seeder to announce to tracker.
	time.Sleep(100 * time.Millisecond)

	sto2 := filestorage.New(where)
	res2 := newResumeFile(t)
	defer res2.Close()

	f.Seek(0, io.SeekStart)
	magnetLink := "magnet:?xt=urn:btih:0a8e2e8c9371a91e9047ed189ceffbc460803262&dn=10mb&tr=http%3A%2F%2F127.0.0.1%3A5000%2Fannounce"
	t2, err := torrent.NewMagnet(magnetLink, 6882, sto2, res2)
	if err != nil {
		t.Fatal(err)
	}
	defer t2.Close()

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
