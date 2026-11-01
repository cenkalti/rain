package allocator_test

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/cenkalti/rain/v2/internal/allocator"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/storage/filestorage"
	"github.com/cenkalti/rain/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runAllocator(t *testing.T, info *metainfo.Info, sto *filestorage.FileStorage) *allocator.Allocator {
	t.Helper()
	a := allocator.New()
	progressC := make(chan allocator.Progress, len(info.Files))
	resultC := make(chan *allocator.Allocator, 1)
	go a.Run(info, sto, progressC, resultC)
	res := <-resultC
	t.Cleanup(func() {
		for _, f := range res.Files {
			if f.Storage != nil {
				_ = f.Storage.Close()
			}
		}
	})
	return res
}

func TestAllocatorCreatesThenFindsFiles(t *testing.T) {
	dir := t.TempDir()
	sto, err := filestorage.New(dir, 0o750)
	require.NoError(t, err)

	info := &metainfo.Info{
		Files: []metainfo.File{
			{Path: "a.bin", Length: 100},
			{Path: filepath.Join("sub", "b.bin"), Length: 50},
		},
	}

	// First allocation: nothing exists yet.
	first := runAllocator(t, info, sto)
	require.NoError(t, first.Error)
	require.Len(t, first.Files, 2)
	assert.True(t, first.HasMissing)
	assert.False(t, first.HasExisting)

	// Second allocation over the same storage: the files now exist.
	second := runAllocator(t, info, sto)
	require.NoError(t, second.Error)
	assert.True(t, second.HasExisting)
	assert.False(t, second.HasMissing)
}

func TestAllocatorPaddingFile(t *testing.T) {
	dir := t.TempDir()
	sto, err := filestorage.New(dir, 0o750)
	require.NoError(t, err)

	info := &metainfo.Info{
		Files: []metainfo.File{
			{Path: "a.bin", Length: 100},
			{Path: "pad", Length: 28, Padding: true},
		},
	}

	res := runAllocator(t, info, sto)
	require.NoError(t, res.Error)
	require.Len(t, res.Files, 2)
	assert.False(t, res.Files[0].Padding)
	assert.True(t, res.Files[1].Padding)
	require.NotNil(t, res.Files[1].Storage) // padding files get an in-memory zero file
}

type countingStorage struct {
	mu     sync.Mutex
	opened int
	closed int
}

func (s *countingStorage) Open(name string, size int64) (storage.File, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opened++
	return &countingFile{s: s}, false, nil
}

func (s *countingStorage) RootDir() string { return "" }

type countingFile struct {
	s *countingStorage
}

func (f *countingFile) ReadAt(p []byte, off int64) (int, error)  { return len(p), nil }
func (f *countingFile) WriteAt(p []byte, off int64) (int, error) { return len(p), nil }

func (f *countingFile) Close() error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.closed++
	return nil
}

func TestAllocatorCloseDuringAllocation(t *testing.T) {
	sto := &countingStorage{}
	info := &metainfo.Info{
		Files: []metainfo.File{
			{Path: "a", Length: 1},
			{Path: "b", Length: 1},
			{Path: "c", Length: 1},
		},
	}
	a := allocator.New()
	progressC := make(chan allocator.Progress)
	go a.Run(info, sto, progressC, make(chan *allocator.Allocator))
	<-progressC
	a.Close()

	assert.Equal(t, 2, sto.opened)
	assert.Equal(t, sto.opened, sto.closed)
}

func TestAllocatorCloseBeforeResult(t *testing.T) {
	sto := &countingStorage{}
	info := &metainfo.Info{
		Files: []metainfo.File{
			{Path: "a", Length: 1},
			{Path: "b", Length: 1},
		},
	}
	a := allocator.New()
	progressC := make(chan allocator.Progress, len(info.Files))
	go a.Run(info, sto, progressC, make(chan *allocator.Allocator))
	<-progressC
	<-progressC
	a.Close()

	assert.Equal(t, 2, sto.opened)
	assert.Equal(t, sto.opened, sto.closed)
}
