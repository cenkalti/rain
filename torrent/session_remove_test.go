package torrent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopDelay is how long the test tracker takes to respond to a "stopped" announce.
const stopDelay = 500 * time.Millisecond

// Removing a running torrent must tell the trackers about it, otherwise the
// final upload/download counts never reach the tracker. The announce must not
// delay the removal itself.
func TestRemoveTorrentAnnouncesStopped(t *testing.T) {
	announces, srv := newTestTracker(t)
	s := newTestSession(t)
	tor := addTorrentWithTracker(t, s, srv.URL)

	assert.Equal(t, "started", recvAnnounce(t, announces).Get("event"))

	start := time.Now()
	require.NoError(t, s.RemoveTorrent(tor.ID(), false))
	assert.Less(t, time.Since(start), stopDelay, "RemoveTorrent must not wait for the tracker")

	q := recvAnnounce(t, announces)
	assert.Equal(t, "stopped", q.Get("event"))
	assert.NotEmpty(t, q.Get("uploaded"))
	assert.NotEmpty(t, q.Get("downloaded"))
}

// The process usually exits right after Session.Close, so the "stopped"
// announces of closed torrents must be delivered before it returns.
func TestSessionCloseWaitsForStoppedAnnounce(t *testing.T) {
	announces, srv := newTestTracker(t)
	s, err := NewSession(newTestConfig(t))
	require.NoError(t, err)
	tor := addTorrentWithTracker(t, s, srv.URL)

	assert.Equal(t, "started", recvAnnounce(t, announces).Get("event"))

	require.NoError(t, s.RemoveTorrent(tor.ID(), false))
	require.NoError(t, s.Close())

	select {
	case q := <-announces:
		assert.Equal(t, "stopped", q.Get("event"))
	default:
		t.Fatal("session closed before the stopped announce was delivered")
	}
}

// newTestTracker starts an HTTP tracker that records the query of each announce
// it answers. A "stopped" announce is answered after stopDelay.
func newTestTracker(t *testing.T) (<-chan url.Values, *httptest.Server) {
	t.Helper()
	announces := make(chan url.Values, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("event") == "stopped" {
			select {
			case <-time.After(stopDelay):
			case <-r.Context().Done():
				return
			}
		}
		announces <- q
		_, _ = w.Write([]byte("d8:intervali3600e5:peers0:e"))
	}))
	t.Cleanup(srv.Close)
	return announces, srv
}

// addTorrentWithTracker adds the sample torrent with the given tracker as its
// only tracker and starts it.
func addTorrentWithTracker(t *testing.T, s *Session, trackerURL string) *Torrent {
	t.Helper()
	f, err := os.Open(torrentFile)
	require.NoError(t, err)
	defer f.Close()

	tor, err := s.AddTorrent(f, &AddTorrentOptions{Stopped: true})
	require.NoError(t, err)
	tor.torrent.trackers = nil
	require.NoError(t, tor.AddTracker(trackerURL+"/announce"))
	require.NoError(t, tor.Start())
	return tor
}

func recvAnnounce(t *testing.T, announces <-chan url.Values) url.Values {
	t.Helper()
	select {
	case q := <-announces:
		return q
	case <-time.After(timeout):
		t.Fatal("tracker was not contacted")
		return nil
	}
}

func TestRemoveTorrentDeletesData(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	name := filepath.Join(tor.Dir(), "a")
	require.NoError(t, os.MkdirAll(tor.Dir(), 0o750))
	require.NoError(t, os.WriteFile(name, nil, 0o600))

	require.NoError(t, s.RemoveTorrent(tor.ID(), false))
	assert.NoDirExists(t, tor.Dir())
}

func TestRemoveTorrentKeepsData(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	name := filepath.Join(tor.Dir(), "a")
	require.NoError(t, os.MkdirAll(tor.Dir(), 0o750))
	require.NoError(t, os.WriteFile(name, nil, 0o600))

	require.NoError(t, s.RemoveTorrent(tor.ID(), true))
	assert.FileExists(t, name)
}

func TestRemoveTorrentDeletesDataInSharedDir(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DataDirIncludesTorrentID = false
	s, err := NewSession(cfg)
	require.NoError(t, err)
	defer s.Close()
	tor := addStoppedTorrent(t, s)
	torrentData := filepath.Join(cfg.DataDir, torrentName)
	other := filepath.Join(cfg.DataDir, "other")
	require.NoError(t, os.MkdirAll(torrentData, 0o750))
	require.NoError(t, os.WriteFile(other, nil, 0o600))

	require.NoError(t, s.RemoveTorrent(tor.ID(), false))
	assert.NoDirExists(t, torrentData)
	assert.FileExists(t, other)
}

type failingRemoveProvider struct {
	err error
}

func (p *failingRemoveProvider) GetStorage(string) (storage.Storage, error) {
	return failingRemoveStorage{p}, nil
}

type failingRemoveStorage struct {
	p *failingRemoveProvider
}

func (s failingRemoveStorage) Open(string, int64) (storage.File, bool, error) {
	return memFile{}, false, nil
}

func (s failingRemoveStorage) RootDir() string { return "" }

func (s failingRemoveStorage) Remove(*storage.Manifest, bool) error { return s.p.err }

type memFile struct{}

func (memFile) ReadAt(p []byte, off int64) (int, error)  { return len(p), nil }
func (memFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }
func (memFile) Close() error                             { return nil }

func TestRemoveTorrentKeepsTorrentWhenStorageFails(t *testing.T) {
	announces, srv := newTestTracker(t)
	provider := &failingRemoveProvider{err: errors.New("remove failed")}
	cfg := newTestConfig(t)
	cfg.CustomStorage = provider
	s, err := NewSession(cfg)
	require.NoError(t, err)
	defer s.Close()
	tor := addTorrentWithTracker(t, s, srv.URL)
	assert.Equal(t, "started", recvAnnounce(t, announces).Get("event"))

	require.ErrorIs(t, s.RemoveTorrent(tor.ID(), false), provider.err)
	assert.Same(t, tor, s.GetTorrent(tor.ID()))
	assert.Contains(t, []Status{Stopping, Stopped}, tor.Stats().Status)
	assert.Equal(t, "stopped", recvAnnounce(t, announces).Get("event"))

	provider.err = nil
	require.NoError(t, s.RemoveTorrent(tor.ID(), false))
	assert.Nil(t, s.GetTorrent(tor.ID()))
}
