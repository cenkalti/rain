package metainfo

import (
	"crypto/sha1"
	"os"
	"path/filepath"
	"testing"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/bencode"
)

func testLog() logger.Logger { return logger.New("metainfo-test") }

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

// repeat builds recognizable content of a given length.
func repeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestNewInfoBytesRejectsNoPaths(t *testing.T) {
	_, err := NewInfoBytes("", nil, false, 0, "", testLog())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no path specified")
}

func TestNewInfoBytesRejectsMissingFile(t *testing.T) {
	_, err := NewInfoBytes("", []string{filepath.Join(t.TempDir(), "nope")}, false, 0, "", testLog())
	assert.Error(t, err)
}

func TestNewInfoBytesRejectsEmptyFile(t *testing.T) {
	p := writeFile(t, filepath.Join(t.TempDir(), "empty.bin"), nil)
	_, err := NewInfoBytes("", []string{p}, false, 0, "", testLog())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files")
}

// With more than one path there is no single parent to make paths relative to,
// so the caller has to supply both a root and a name.
func TestNewInfoBytesRequiresRootAndNameForMultiplePaths(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, filepath.Join(dir, "a.bin"), repeat('a', 100))
	b := writeFile(t, filepath.Join(dir, "b.bin"), repeat('b', 100))

	_, err := NewInfoBytes("", []string{a, b}, false, 0, "name", testLog())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no root specified")

	_, err = NewInfoBytes(dir, []string{a, b}, false, 0, "", testLog())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no name specified")
}

func TestNewInfoBytesRejectsUnalignedPieceLength(t *testing.T) {
	p := writeFile(t, filepath.Join(t.TempDir(), "a.bin"), repeat('a', 100))

	_, err := NewInfoBytes("", []string{p}, false, 1000, "", testLog())
	assert.ErrorIs(t, err, errPieceLength)

	// A multiple of 16K is accepted.
	_, err = NewInfoBytes("", []string{p}, false, 32<<10, "", testLog())
	assert.NoError(t, err)
}

func TestNewInfoBytesSingleFile(t *testing.T) {
	data := repeat('x', 40000)
	p := writeFile(t, filepath.Join(t.TempDir(), "movie.bin"), data)

	b, err := NewInfoBytes("", []string{p}, false, 16<<10, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.Equal(t, "movie.bin", i.Name, "the name defaults to the file's base name")
	assert.Equal(t, int64(len(data)), i.Length)
	assert.Equal(t, uint32(16<<10), i.PieceLength)
	assert.Equal(t, uint32(3), i.NumPieces, "40000 bytes needs 3 pieces of 16K")
	assert.Equal(t, []File{{Length: int64(len(data)), Path: "movie.bin"}}, i.Files)
	assert.False(t, i.Private)
}

func TestNewInfoBytesExplicitName(t *testing.T) {
	p := writeFile(t, filepath.Join(t.TempDir(), "movie.bin"), repeat('x', 100))

	b, err := NewInfoBytes("", []string{p}, false, 16<<10, "chosen", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.Equal(t, "chosen", i.Name)
}

func TestNewInfoBytesPrivateFlag(t *testing.T) {
	p := writeFile(t, filepath.Join(t.TempDir(), "a.bin"), repeat('a', 100))

	b, err := NewInfoBytes("", []string{p}, true, 16<<10, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.True(t, i.Private)
}

func TestNewInfoBytesDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "album")
	writeFile(t, filepath.Join(dir, "01.bin"), repeat('1', 100))
	writeFile(t, filepath.Join(dir, "02.bin"), repeat('2', 200))
	writeFile(t, filepath.Join(dir, "art", "cover.bin"), repeat('3', 300))

	b, err := NewInfoBytes("", []string{dir}, false, 16<<10, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.Equal(t, "album", i.Name)
	assert.Equal(t, int64(600), i.Length)
	assert.Equal(t, []File{
		{Length: 100, Path: "album/01.bin"},
		{Length: 200, Path: "album/02.bin"},
		{Length: 300, Path: "album/art/cover.bin"},
	}, i.Files, "paths are relative to the directory and walked in lexical order")
}

func TestNewInfoBytesMultiplePaths(t *testing.T) {
	root := t.TempDir()
	a := writeFile(t, filepath.Join(root, "a.bin"), repeat('a', 100))
	b := writeFile(t, filepath.Join(root, "b.bin"), repeat('b', 200))

	out, err := NewInfoBytes(root, []string{a, b}, false, 16<<10, "both", testLog())
	require.NoError(t, err)

	i, err := NewInfo(out, true, true)
	require.NoError(t, err)
	assert.Equal(t, "both", i.Name)
	assert.Equal(t, int64(300), i.Length)
	assert.Equal(t, []File{
		{Length: 100, Path: "both/a.bin"},
		{Length: 200, Path: "both/b.bin"},
	}, i.Files)
}

// The piece hashes are the whole point of the info dict: if they are wrong,
// every peer rejects the data. Compute them independently here rather than
// trusting the writer.
func TestNewInfoBytesPieceHashesMatchTheData(t *testing.T) {
	const pieceLength = 16 << 10
	data := make([]byte, pieceLength*2+500)
	for i := range data {
		data[i] = byte(i % 251)
	}
	p := writeFile(t, filepath.Join(t.TempDir(), "data.bin"), data)

	b, err := NewInfoBytes("", []string{p}, false, pieceLength, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	require.Equal(t, uint32(3), i.NumPieces)

	for idx := range int(i.NumPieces) {
		begin := idx * pieceLength
		end := min(begin+pieceLength, len(data))
		want := sha1.Sum(data[begin:end])
		assert.Equal(t, want[:], i.PieceHash(uint32(idx)), "piece %d hash", idx)
	}
}

// Pieces span file boundaries, so the hash of the first piece of a two-file
// torrent covers the end of one file and the start of the next.
func TestNewInfoBytesPiecesSpanFiles(t *testing.T) {
	const pieceLength = 16 << 10
	first := repeat('a', pieceLength-100)
	second := repeat('b', 300)

	root := t.TempDir()
	dir := filepath.Join(root, "set")
	writeFile(t, filepath.Join(dir, "1.bin"), first)
	writeFile(t, filepath.Join(dir, "2.bin"), second)

	b, err := NewInfoBytes("", []string{dir}, false, pieceLength, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	require.Equal(t, uint32(2), i.NumPieces)
	assert.Equal(t, int64(len(first)+len(second)), i.Length)

	joined := append(append([]byte{}, first...), second...)
	want0 := sha1.Sum(joined[:pieceLength])
	want1 := sha1.Sum(joined[pieceLength:])
	assert.Equal(t, want0[:], i.PieceHash(0), "the first piece crosses into the second file")
	assert.Equal(t, want1[:], i.PieceHash(1))
}

func TestNewInfoBytesCalculatesPieceLengthWhenZero(t *testing.T) {
	p := writeFile(t, filepath.Join(t.TempDir(), "a.bin"), repeat('a', 100))

	b, err := NewInfoBytes("", []string{p}, false, 0, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.Equal(t, uint32(32<<10), i.PieceLength, "a small torrent gets the 32K minimum")
}

// A single-file torrent must not carry a files list, and a multi-file one must
// not carry a top-level length. Readers use the presence of "files" to decide
// which mode they are in.
func TestNewInfoBytesUsesCorrectModeKeys(t *testing.T) {
	root := t.TempDir()
	single := writeFile(t, filepath.Join(root, "one.bin"), repeat('a', 100))
	dir := filepath.Join(root, "many")
	writeFile(t, filepath.Join(dir, "x.bin"), repeat('x', 100))

	decode := func(b []byte) (hasLength, hasFiles bool) {
		var m map[string]bencode.RawMessage
		require.NoError(t, bencode.DecodeBytes(b, &m))
		_, hasLength = m["length"]
		_, hasFiles = m["files"]
		return
	}

	b, err := NewInfoBytes("", []string{single}, false, 16<<10, "", testLog())
	require.NoError(t, err)
	hasLength, hasFiles := decode(b)
	assert.True(t, hasLength, "single-file mode sets length")
	assert.False(t, hasFiles, "single-file mode omits files")

	b, err = NewInfoBytes("", []string{dir}, false, 16<<10, "", testLog())
	require.NoError(t, err)
	hasLength, hasFiles = decode(b)
	assert.False(t, hasLength, "multi-file mode omits length")
	assert.True(t, hasFiles, "multi-file mode sets files")
}

func TestNewInfoBytesSkipsEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "set")
	writeFile(t, filepath.Join(dir, "a.bin"), repeat('a', 100))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o755))

	b, err := NewInfoBytes("", []string{dir}, false, 16<<10, "", testLog())
	require.NoError(t, err)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)
	assert.Equal(t, []File{{Length: 100, Path: "set/a.bin"}}, i.Files,
		"a directory contributes no entry of its own")
}
