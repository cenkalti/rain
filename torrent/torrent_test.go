package torrent

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/webseedsource"
	fhttp "github.com/chihaya/chihaya/frontend/http"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/storage"
	_ "github.com/chihaya/chihaya/storage/memory"
	"github.com/fortytw2/leaktest"
	cp "github.com/otiai10/copy"
	"github.com/stretchr/testify/assert"
)

var (
	torrentFile           = filepath.Join("testdata", "sample_torrent.torrent")
	torrentInfoHashString = "4242e334070406956b87c25f7c36251d32743461"
	torrentMagnetLink     = "magnet:?xt=urn:btih:" + torrentInfoHashString
	torrentDataDir        = "testdata"
	torrentName           = "sample_torrent"
	timeout               = 10 * time.Second
)

func init() {
	logger.SetDebug()
}

func newTestSession(t *testing.T) (*Session, func()) {
	tmp, closeTmp := tempdir(t)
	cfg := DefaultConfig
	cfg.Database = filepath.Join(tmp, "session.db")
	cfg.DataDir = tmp
	cfg.DHTEnabled = false
	cfg.PEXEnabled = false
	cfg.RPCEnabled = false
	cfg.Host = "127.0.0.1"
	s, err := NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, func() {
		err := s.Close()
		if err != nil {
			t.Fatal(err)
		}
		closeTmp()
	}
}

func CopyDir(src, dst string) error {
	cmd := exec.Command("cp", "-a", src, dst)
	return cmd.Run()
}

func seeder(t *testing.T, clearTrackers bool) (addr string, c func()) {
	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s, closeSession := newTestSession(t)
	opt := &AddTorrentOptions{Stopped: true}
	tor, err := s.AddTorrent(f, opt)
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(torrentDataDir, torrentName)
	dst := filepath.Join(s.config.DataDir, tor.ID(), torrentName)
	err = os.Mkdir(filepath.Join(s.config.DataDir, tor.ID()), os.ModeDir|s.config.FilePermissions)
	if err != nil {
		t.Fatal(err)
	}
	err = cp.Copy(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if clearTrackers {
		tor.torrent.trackers = nil
	}
	tor.Start()
	var port int
	select {
	case port = <-tor.torrent.NotifyListen():
	case err = <-tor.torrent.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("seeder is not ready")
	}
	return "127.0.0.1:" + strconv.Itoa(port), func() {
		closeSession()
	}
}

func tempdir(t *testing.T) (string, func()) {
	where, err := os.MkdirTemp("", "rain-")
	if err != nil {
		t.Fatal(err)
	}
	return where, func() {
		err = os.RemoveAll(where)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadMagnet(t *testing.T) {
	defer leaktest.Check(t)()
	addr, cl := seeder(t, true)
	defer cl()
	s, closeSession := newTestSession(t)
	defer closeSession()

	tor, err := s.AddURI(torrentMagnetLink+"&x.pe="+addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertCompleted(t, tor)
}

func TestDownloadTorrent(t *testing.T) {
	// TODO defer leaktest.Check(t)()
	defer startHTTPTracker(t)()

	_, cl := seeder(t, false)
	defer cl()

	s, closeSession := newTestSession(t)
	defer closeSession()

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tor, err := s.AddTorrent(f, nil)
	if err != nil {
		t.Fatal(err)
	}

	assertCompleted(t, tor)
}

func TestTorrentRootDirectory(t *testing.T) {
	defer leaktest.Check(t)()
	addr, cl := seeder(t, true)
	defer cl()
	s, closeSession := newTestSession(t)
	defer closeSession()

	tor, err := s.AddURI(torrentMagnetLink+"&x.pe="+addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, filepath.Join(s.config.DataDir, tor.ID()), tor.RootDirectory())
	assertCompleted(t, tor)
}

func TestTorrentFiles(t *testing.T) {
	defer leaktest.Check(t)()
	addr, cl := seeder(t, true)
	defer cl()
	s, closeSession := newTestSession(t)
	defer closeSession()

	tor, err := s.AddURI(torrentMagnetLink+"&x.pe="+addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tor.FilePaths()
	assert.EqualError(t, err, "torrent metadata not ready")
	_, err = tor.Files()
	assert.EqualError(t, err, "torrent not running so file stats unavailable")
	waitForMetadata(t, tor)
	files, err := tor.FilePaths()
	assert.NoError(t, err)
	assert.Equal(t, 6, len(files))
	assert.Equal(t, "sample_torrent/data/file1.bin", files[0])
	assertCompleted(t, tor)
	// So that we're in running state again, which should allow
	// Files() to return data.
	tor.Start()
	waitForStart(t, tor)
	fileStats, err := tor.Files()
	assert.NoError(t, err)
	assert.Equal(t, 6, len(fileStats))
	assert.Equal(t, "sample_torrent/data/file1.bin", fileStats[0].Path())
	assert.Equal(t, int64(10240), fileStats[0].Stats().BytesTotal)
	assert.Equal(t, int64(10240), fileStats[0].Stats().BytesCompleted)
	assert.Equal(t, int64(10485760), fileStats[2].Stats().BytesCompleted)
	assertCompleted(t, tor)
}

func startHTTPTracker(t *testing.T) (stop func()) {
	responseConfig := middleware.ResponseConfig{
		AnnounceInterval: time.Minute,
	}
	ps, err := storage.NewPeerStore("memory", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	lgc := middleware.NewLogic(responseConfig, ps, nil, nil)
	fe, err := fhttp.NewFrontend(lgc, fhttp.Config{
		Addr:         "127.0.0.1:5000",
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		errC := fe.Stop()
		err := <-errC
		if err != nil {
			t.Fatal(err)
		}
	}
}

func webseed(t *testing.T) (port int, c func()) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = l.Addr().(*net.TCPAddr).Port
	servingDone := make(chan struct{})
	srv := &http.Server{Handler: http.FileServer(http.Dir("./testdata"))}
	go func() {
		srv.Serve(l)
		close(servingDone)
	}()
	return port, func() {
		srv.Close()
		l.Close()
		<-servingDone
	}
}

func TestDownloadWebseed(t *testing.T) {
	defer leaktest.Check(t)()
	port1, close1 := webseed(t)
	defer close1()
	port2, close2 := webseed(t)
	defer close2()
	addr, cl := seeder(t, true)
	defer cl()
	s, closeSession := newTestSession(t)
	defer closeSession()

	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	opt := &AddTorrentOptions{Stopped: true}
	tor, err := s.AddTorrent(f, opt)
	if err != nil {
		t.Fatal(err)
	}
	tor.torrent.webseedSources = webseedsource.NewList([]string{
		"http://127.0.0.1:" + strconv.Itoa(port1),
		"http://127.0.0.1:" + strconv.Itoa(port2),
	})
	tor.torrent.webseedClient = http.DefaultClient
	tor.Start()
	tor.AddPeer(addr)

	assertCompleted(t, tor)
}

func assertCompleted(t *testing.T, tor *Torrent) {
	t2 := tor.torrent
	select {
	case <-t2.NotifyComplete():
	case err := <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("download did not finish")
	}
	dir1 := filepath.Join(torrentDataDir, torrentName)
	dir2 := filepath.Join(tor.torrent.session.config.DataDir, tor.ID(), torrentName)
	cmd := exec.Command("diff", "-rq", dir1, dir2)
	err := cmd.Run()
	if err != nil {
		t.Fatal(err)
	}
}

func waitForMetadata(t *testing.T, tor *Torrent) {
	t2 := tor.torrent
	select {
	case <-t2.NotifyMetadata():
	case err := <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("metadata did not finish downloading")
	}
}

func waitForStart(t *testing.T, tor *Torrent) {
	t2 := tor.torrent
	select {
	case <-t2.NotifyListen():
	case err := <-t2.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("start dit not finish")
	}
}
