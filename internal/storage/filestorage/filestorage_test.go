package filestorage

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorageOpenCreateAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0o750)
	require.NoError(t, err)
	assert.Equal(t, dir, s.RootDir())

	// First open creates the file (and its containing subdirectory).
	name := filepath.Join("sub", "a.bin")
	f, exists, err := s.Open(name, 16)
	require.NoError(t, err)
	assert.False(t, exists)

	data := []byte("0123456789ABCDEF")
	n, err := f.WriteAt(data, 0)
	require.NoError(t, err)
	assert.Equal(t, 16, n)

	got := make([]byte, 16)
	_, err = f.ReadAt(got, 0)
	require.NoError(t, err)
	assert.Equal(t, data, got)
	require.NoError(t, f.Close())

	// Reopening at the same size reports exists=true and preserves content.
	f2, exists, err := s.Open(name, 16)
	require.NoError(t, err)
	assert.True(t, exists)
	got2 := make([]byte, 16)
	_, err = f2.ReadAt(got2, 0)
	require.NoError(t, err)
	assert.Equal(t, data, got2)
	require.NoError(t, f2.Close())
}

func TestFileStorageOpenTruncatesOnSizeChange(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0o750)
	require.NoError(t, err)

	f, _, err := s.Open("a.bin", 10)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Reopening with a larger size grows (truncates) the file to match.
	f2, exists, err := s.Open("a.bin", 20)
	require.NoError(t, err)
	assert.True(t, exists)
	got := make([]byte, 20)
	_, err = f2.ReadAt(got, 0)
	require.NoError(t, err)
	require.NoError(t, f2.Close())
}
