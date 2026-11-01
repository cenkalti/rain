package filestorage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cenkalti/rain/v2/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorageOpenCreateAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, 0o750, true)
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
	s, err := New(dir, 0o750, true)
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

func writeFile(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o750))
	require.NoError(t, os.WriteFile(name, nil, 0o600))
}

func TestFileStorageRemoveOwnedDest(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "id")
	s, err := New(dest, 0o750, true)
	require.NoError(t, err)
	writeFile(t, filepath.Join(dest, "name", "a"))

	require.NoError(t, s.Remove(nil, false))
	assert.NoDirExists(t, dest)
}

func TestFileStorageRemoveSharedDest(t *testing.T) {
	dest := t.TempDir()
	s, err := New(dest, 0o750, false)
	require.NoError(t, err)
	writeFile(t, filepath.Join(dest, "name", "a"))
	writeFile(t, filepath.Join(dest, "name", "sub", "b"))
	writeFile(t, filepath.Join(dest, "other"))
	m := &storage.Manifest{Files: []storage.ManifestFile{
		{Path: filepath.Join("name", "a")},
		{Path: filepath.Join(".pad", "1"), Padding: true},
		{Path: filepath.Join("name", "sub", "b")},
	}}

	require.NoError(t, s.Remove(m, false))
	assert.NoDirExists(t, filepath.Join(dest, "name"))
	assert.FileExists(t, filepath.Join(dest, "other"))

	require.NoError(t, s.Remove(nil, false))
	assert.FileExists(t, filepath.Join(dest, "other"))
}

func TestFileStorageRemoveKeepData(t *testing.T) {
	dest := t.TempDir()
	s, err := New(dest, 0o750, true)
	require.NoError(t, err)
	writeFile(t, filepath.Join(dest, "a"))

	require.NoError(t, s.Remove(nil, true))
	assert.FileExists(t, filepath.Join(dest, "a"))
}

func TestFileStorageRemoveRejectsNonLocalPath(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "data")
	s, err := New(dest, 0o750, false)
	require.NoError(t, err)
	writeFile(t, filepath.Join(parent, "outside"))
	m := &storage.Manifest{Files: []storage.ManifestFile{{Path: filepath.Join("..", "outside")}}}

	require.Error(t, s.Remove(m, false))
	assert.FileExists(t, filepath.Join(parent, "outside"))
}
