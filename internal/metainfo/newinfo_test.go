package metainfo

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/bencode"
)

// Info dicts arrive from strangers, so these fixtures are built as raw maps
// rather than through a typed struct: a test has to be able to express a dict
// the encoder would never produce.

func encodeInfo(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := bencode.EncodeBytes(m)
	require.NoError(t, err)
	return b
}

// pieceData returns n concatenated SHA-1 slots.
func pieceData(n int) []byte { return make([]byte, n*sha1.Size) }

func singleFileInfo() map[string]any {
	return map[string]any{
		"piece length": 16384,
		"pieces":       pieceData(1),
		"name":         "file.txt",
		"length":       16384,
	}
}

func multiFileInfo() map[string]any {
	return map[string]any{
		"piece length": 16384,
		"pieces":       pieceData(1),
		"name":         "dir",
		"files": []any{
			map[string]any{"length": 10000, "path": []any{"a.txt"}},
			map[string]any{"length": 6384, "path": []any{"sub", "b.txt"}},
		},
	}
}

func TestNewInfoSingleFile(t *testing.T) {
	b := encodeInfo(t, singleFileInfo())
	i, err := NewInfo(b, true, true)
	require.NoError(t, err)

	assert.Equal(t, uint32(16384), i.PieceLength)
	assert.Equal(t, uint32(1), i.NumPieces)
	assert.Equal(t, "file.txt", i.Name)
	assert.Equal(t, int64(16384), i.Length)
	assert.Zero(t, i.Padding)
	assert.False(t, i.Private)
	assert.Equal(t, b, i.Bytes, "the original bytes are retained for re-sharing")
	assert.Equal(t, []File{{Length: 16384, Path: "file.txt"}}, i.Files)
}

func TestNewInfoMultiFile(t *testing.T) {
	i, err := NewInfo(encodeInfo(t, multiFileInfo()), true, true)
	require.NoError(t, err)

	assert.Equal(t, "dir", i.Name)
	assert.Equal(t, int64(16384), i.Length, "length is the sum of the files")
	assert.Equal(t, []File{
		{Length: 10000, Path: "dir/a.txt"},
		{Length: 6384, Path: "dir/sub/b.txt"},
	}, i.Files, "every path is rooted at the torrent name")
}

func TestNewInfoRejectsMalformedBencode(t *testing.T) {
	full := encodeInfo(t, singleFileInfo())
	cases := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"not bencode", []byte("hello")},
		{"truncated", full[:len(full)/2]},
		{"missing terminator", full[:len(full)-1]},
		{"a list, not a dict", []byte("le")},
		{"an integer", []byte("i42e")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewInfo(c.in, true, true)
			assert.Error(t, err)
		})
	}
}

func TestNewInfoRejectsBadPieceGeometry(t *testing.T) {
	cases := []struct {
		name string
		mod  func(m map[string]any)
		want error
	}{
		{
			"zero piece length",
			func(m map[string]any) { m["piece length"] = 0 },
			errZeroPieceLength,
		},
		{
			"missing piece length",
			func(m map[string]any) { delete(m, "piece length") },
			errZeroPieceLength,
		},
		{
			"pieces not a multiple of 20",
			func(m map[string]any) { m["pieces"] = make([]byte, 25) },
			errInvalidPieceData,
		},
		{
			"no pieces",
			func(m map[string]any) { m["pieces"] = []byte{} },
			errZeroPieces,
		},
		{
			// 1 piece covers 16384 bytes, so a zero length leaves a whole
			// unexplained piece.
			"length too small for the piece count",
			func(m map[string]any) { m["length"] = 0 },
			errInvalidPieceData,
		},
		{
			"length exceeds what the pieces cover",
			func(m map[string]any) { m["length"] = 16385 },
			errInvalidPieceData,
		},
		{
			"two pieces but only one piece of data",
			func(m map[string]any) { m["pieces"] = pieceData(2) },
			errInvalidPieceData,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := singleFileInfo()
			c.mod(m)
			_, err := NewInfo(encodeInfo(t, m), true, true)
			assert.ErrorIs(t, err, c.want)
		})
	}
}

func TestNewInfoAcceptsPartialLastPiece(t *testing.T) {
	// A final short piece is normal: 2 pieces may cover as little as
	// pieceLength+1 bytes.
	for _, length := range []int64{16385, 20000, 32768} {
		m := singleFileInfo()
		m["pieces"] = pieceData(2)
		m["length"] = length
		i, err := NewInfo(encodeInfo(t, m), true, true)
		require.NoError(t, err, "length %d", length)
		assert.Equal(t, uint32(2), i.NumPieces)
		assert.Equal(t, length, i.Length)
	}
}

// ".." as a whole path element is rejected outright.
func TestNewInfoRejectsDotDotPathElement(t *testing.T) {
	for _, elem := range []string{"..", " .. ", "\t..", ".. "} {
		m := multiFileInfo()
		m["files"] = []any{
			map[string]any{"length": 16384, "path": []any{elem, "evil.txt"}},
		}
		_, err := NewInfo(encodeInfo(t, m), true, true)
		require.Error(t, err, "path element %q must be rejected", elem)
		assert.Contains(t, err.Error(), "invalid file name")
	}
}

// A separator embedded in a single element is not a traversal: cleanName turns
// it into an underscore, so the element collapses to one harmless file name.
// This is the real defense, and it is worth pinning because the ".." check
// above would not catch any of these on its own.
func TestNewInfoNeutralizesEmbeddedSeparators(t *testing.T) {
	cases := []struct {
		elem string
		want string
	}{
		{"../evil.txt", "dir/.._evil.txt"},
		{"../../etc/passwd", "dir/.._.._etc_passwd"},
		{"/etc/passwd", "dir/_etc_passwd"},
		{"a/b", "dir/a_b"},
	}
	for _, c := range cases {
		t.Run(c.elem, func(t *testing.T) {
			m := multiFileInfo()
			m["files"] = []any{
				map[string]any{"length": 16384, "path": []any{c.elem}},
			}
			i, err := NewInfo(encodeInfo(t, m), true, true)
			require.NoError(t, err)
			assert.Equal(t, c.want, i.Files[0].Path)
			assertNoTraversal(t, i.Files[0].Path)
		})
	}
}

// assertNoTraversal checks that no element of a path is "..", which is the
// property that keeps a file inside the torrent directory. A literal ".." as
// part of a longer element, such as ".._evil.txt", is an ordinary file name.
func assertNoTraversal(t *testing.T, p string) {
	t.Helper()
	for elem := range strings.SplitSeq(p, "/") {
		assert.NotEqual(t, "..", elem, "path %q escapes the torrent directory", p)
	}
}

// Info.Name is the raw name from the torrent file; only Files[].Path is passed
// through cleanName. Callers that build a filesystem path must therefore use
// Files[].Path, or sanitize Name themselves.
//
// This boundary is load-bearing: filestorage joins Files[].Path under its own
// root, so writing data is safe, but Session.stopAndRemoveData joins
// Info.Name instead when data-dir-includes-torrent-id is off.
func TestInfoNameIsNotSanitizedButFilePathsAre(t *testing.T) {
	m := multiFileInfo()
	m["name"] = "../escape"
	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)

	assert.Equal(t, "../escape", i.Name, "Name is returned verbatim")

	for _, f := range i.Files {
		assertNoTraversal(t, f.Path)
	}
	assert.Equal(t, ".._escape/a.txt", i.Files[0].Path)
	assert.Equal(t, ".._escape/sub/b.txt", i.Files[1].Path)
}

func TestNewInfoRejectsDuplicatePaths(t *testing.T) {
	m := multiFileInfo()
	m["files"] = []any{
		map[string]any{"length": 8192, "path": []any{"same.txt"}},
		map[string]any{"length": 8192, "path": []any{"same.txt"}},
	}
	_, err := NewInfo(encodeInfo(t, m), true, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate file name")
}

// Two distinct names that clean to the same thing still collide on disk.
func TestNewInfoRejectsPathsThatCleanToTheSameName(t *testing.T) {
	m := multiFileInfo()
	m["files"] = []any{
		map[string]any{"length": 8192, "path": []any{"a/b"}},
		map[string]any{"length": 8192, "path": []any{"a_b"}},
	}
	_, err := NewInfo(encodeInfo(t, m), true, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate file name")
}

func TestNewInfoPaddingFiles(t *testing.T) {
	padded := func() map[string]any {
		m := multiFileInfo()
		m["files"] = []any{
			map[string]any{"length": 10000, "path": []any{"a.txt"}},
			map[string]any{"length": 6384, "path": []any{".pad", "6384"}, "attr": "p"},
		}
		return m
	}

	t.Run("attr p marks a pad file", func(t *testing.T) {
		i, err := NewInfo(encodeInfo(t, padded()), true, true)
		require.NoError(t, err)
		assert.False(t, i.Files[0].Padding)
		assert.True(t, i.Files[1].Padding)
		assert.Equal(t, int64(6384), i.Padding)
		assert.Equal(t, int64(16384), i.Length, "pad bytes still count toward the total")
	})

	t.Run("BitComet naming convention is honored", func(t *testing.T) {
		m := multiFileInfo()
		m["files"] = []any{
			map[string]any{"length": 10000, "path": []any{"a.txt"}},
			map[string]any{"length": 6384, "path": []any{"_____padding_file_0"}},
		}
		i, err := NewInfo(encodeInfo(t, m), true, true)
		require.NoError(t, err)
		assert.True(t, i.Files[1].Padding)
		assert.Equal(t, int64(6384), i.Padding)
	})

	// Padding is accounted for on Info even when the caller opts out of
	// treating pad files specially; only the per-file flag follows the arg.
	t.Run("pad=false clears the per-file flag but not Info.Padding", func(t *testing.T) {
		i, err := NewInfo(encodeInfo(t, padded()), true, false)
		require.NoError(t, err)
		assert.False(t, i.Files[1].Padding)
		assert.Equal(t, int64(6384), i.Padding)
	})

	// BEP 47 lets pad files share a path, so the uniqueness check skips them.
	t.Run("duplicate pad paths are allowed", func(t *testing.T) {
		m := multiFileInfo()
		m["files"] = []any{
			map[string]any{"length": 4096, "path": []any{"a.txt"}},
			map[string]any{"length": 6144, "path": []any{".pad", "x"}, "attr": "p"},
			map[string]any{"length": 6144, "path": []any{".pad", "x"}, "attr": "p"},
		}
		_, err := NewInfo(encodeInfo(t, m), true, true)
		assert.NoError(t, err)
	})

	// With pad=false those same files are ordinary duplicates again.
	t.Run("duplicate pad paths collide when pad=false", func(t *testing.T) {
		m := multiFileInfo()
		m["files"] = []any{
			map[string]any{"length": 4096, "path": []any{"a.txt"}},
			map[string]any{"length": 6144, "path": []any{".pad", "x"}, "attr": "p"},
			map[string]any{"length": 6144, "path": []any{".pad", "x"}, "attr": "p"},
		}
		_, err := NewInfo(encodeInfo(t, m), true, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate file name")
	})
}

func TestNewInfoHashIsOverTheRawBytes(t *testing.T) {
	b := encodeInfo(t, singleFileInfo())
	i, err := NewInfo(b, true, true)
	require.NoError(t, err)

	want := sha1.Sum(b)
	assert.Equal(t, want[:], i.Hash[:])
	assert.Len(t, hex.EncodeToString(i.Hash[:]), 40)
}

// The name key is optional; without it the info hash stands in, so the torrent
// still lands in a directory of its own.
func TestNewInfoNameFallsBackToInfoHash(t *testing.T) {
	m := singleFileInfo()
	delete(m, "name")
	b := encodeInfo(t, m)

	i, err := NewInfo(b, true, true)
	require.NoError(t, err)

	want := sha1.Sum(b)
	assert.Equal(t, hex.EncodeToString(want[:]), i.Name)
	assert.Equal(t, hex.EncodeToString(want[:]), i.Files[0].Path)
}

func TestNewInfoPrivateField(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want bool
	}{
		{"absent", nil, false},
		{"int 1", 1, true},
		{"int 0", 0, false},
		{"int 2", 2, true},
		{"negative", -1, true},
		{"string 1", "1", true},
		{"string 0", "0", false},
		{"empty string", "", false},
		{"string yes", "yes", true},
		{"unparseable type is treated as private", []any{1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := singleFileInfo()
			if c.val != nil {
				m["private"] = c.val
			}
			i, err := NewInfo(encodeInfo(t, m), true, true)
			require.NoError(t, err)
			assert.Equal(t, c.want, i.Private)
		})
	}
}

func TestNewInfoUTF8Overrides(t *testing.T) {
	m := multiFileInfo()
	m["name"] = "legacy"
	m["name.utf-8"] = "modern"
	m["files"] = []any{
		map[string]any{
			"length":     16384,
			"path":       []any{"legacy.txt"},
			"path.utf-8": []any{"modern.txt"},
		},
	}

	t.Run("utf8 true prefers the utf-8 keys", func(t *testing.T) {
		i, err := NewInfo(encodeInfo(t, m), true, true)
		require.NoError(t, err)
		assert.Equal(t, "modern", i.Name)
		assert.Equal(t, "modern/modern.txt", i.Files[0].Path)
	})

	t.Run("utf8 false keeps the legacy keys", func(t *testing.T) {
		i, err := NewInfo(encodeInfo(t, m), false, true)
		require.NoError(t, err)
		assert.Equal(t, "legacy", i.Name)
		assert.Equal(t, "legacy/legacy.txt", i.Files[0].Path)
	})
}

func TestNewInfoTrimsOverlongNames(t *testing.T) {
	long := strings.Repeat("a", 300) + ".txt"
	m := singleFileInfo()
	m["name"] = long

	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)
	assert.Len(t, i.Files[0].Path, 255, "the on-disk name is capped")
	assert.True(t, strings.HasSuffix(i.Files[0].Path, ".txt"), "the extension survives trimming")
}

func TestNewInfoReplacesInvalidUTF8InName(t *testing.T) {
	m := singleFileInfo()
	m["name"] = "bad\xff\xfename.txt"

	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(i.Files[0].Path, "bad"))
	assert.NotContains(t, i.Files[0].Path, "\xff")
}

func TestPieceHash(t *testing.T) {
	m := singleFileInfo()
	m["pieces"] = pieceData(3)
	m["length"] = 16384 * 3
	// Give each slot a recognizable first byte.
	p := m["pieces"].([]byte)
	p[0], p[sha1.Size], p[2*sha1.Size] = 1, 2, 3

	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)
	require.Equal(t, uint32(3), i.NumPieces)

	for idx, first := range []byte{1, 2, 3} {
		h := i.PieceHash(uint32(idx))
		assert.Len(t, h, sha1.Size)
		assert.Equal(t, first, h[0], "piece %d", idx)
	}
}

func TestNewInfoEmptyFileList(t *testing.T) {
	// An empty files list is not multi-file mode, so the top-level length
	// applies and the torrent is treated as a single file.
	m := singleFileInfo()
	m["files"] = []any{}

	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(16384), i.Length)
	assert.Equal(t, []File{{Length: 16384, Path: "file.txt"}}, i.Files)
}

func TestNewInfoZeroLengthFileInMultiFile(t *testing.T) {
	m := multiFileInfo()
	m["files"] = []any{
		map[string]any{"length": 16384, "path": []any{"a.txt"}},
		map[string]any{"length": 0, "path": []any{"empty.txt"}},
	}
	i, err := NewInfo(encodeInfo(t, m), true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(16384), i.Length)
	assert.Equal(t, int64(0), i.Files[1].Length)
}
