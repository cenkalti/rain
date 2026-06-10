package allocator_test

import (
	"path/filepath"
	"testing"

	"github.com/cenkalti/rain/v2/internal/allocator"
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/storage/filestorage"
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
