package torrent

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sparseProvider keeps files in memory. Files always exist, and reading a byte
// that was never written fails with storage.ErrNotWritten.
type sparseProvider struct {
	mu    sync.Mutex
	files map[string]*sparseFile
}

func (p *sparseProvider) GetStorage(string) (storage.Storage, error) { return p, nil }

func (p *sparseProvider) Open(name string, size int64) (storage.File, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.files[name]
	if !ok {
		f = &sparseFile{data: make([]byte, size), written: make([]bool, size)}
		p.files[name] = f
	}
	return f, true, nil
}

func (p *sparseProvider) RootDir() string { return "" }

type sparseFile struct {
	mu      sync.Mutex
	data    []byte
	written []bool
}

func (f *sparseFile) ReadAt(b []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.written[off : off+int64(len(b))] {
		if !w {
			return 0, storage.ErrNotWritten
		}
	}
	return copy(b, f.data[off:]), nil
}

func (f *sparseFile) WriteAt(b []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range b {
		f.written[off+int64(i)] = true
	}
	return copy(f.data[off:], b), nil
}

func (f *sparseFile) Close() error { return nil }

func TestDownloadPiecesNotWritten(t *testing.T) {
	addr := seeder(t, true)
	p := &sparseProvider{files: make(map[string]*sparseFile)}
	cfg := newTestConfig(t)
	cfg.CustomStorage = p
	s, err := NewSession(cfg)
	require.NoError(t, err)
	defer s.Close()
	f, err := os.Open(torrentFile)
	require.NoError(t, err)
	defer f.Close()
	tor, err := s.AddTorrent(f, &AddTorrentOptions{Stopped: true})
	require.NoError(t, err)
	tor.torrent.trackers = nil
	require.NoError(t, tor.Start())
	require.NoError(t, tor.AddPeer(addr))

	select {
	case <-tor.torrent.NotifyComplete():
	case err = <-tor.torrent.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("download did not finish")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.files)
	for name, sf := range p.files {
		want, err := os.ReadFile(filepath.Join(torrentDataDir, name))
		require.NoError(t, err)
		assert.Equal(t, want, sf.data, name)
	}
}
