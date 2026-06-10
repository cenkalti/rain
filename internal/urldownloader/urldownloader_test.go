package urldownloader

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/bufferpool"
	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunPadding(t *testing.T) {
	// Files have position-varying contents so that wrong offsets in piece
	// buffers are detected.
	newFile := func(base byte, length int) []byte {
		b := make([]byte, length)
		for i := range b {
			b[i] = base + byte(i)
		}
		return b
	}
	fileA := newFile(0x10, 10)
	fileB := newFile(0x30, 20)
	fileC := newFile(0x60, 4)
	// Names are relative paths with subdirectories, built with filepath.Join
	// like metainfo does. One segment contains a space to verify that
	// segments are still escaped individually.
	fileAName := filepath.Join("d", "fileA")
	fileBName := filepath.Join("d", "fileB")
	fileCName := filepath.Join("d", "sub", "file c")
	files := map[string][]byte{fileAName: fileA, fileBName: fileB, fileCName: fileC}

	// Padding files are not present on the server, like real webseed servers.
	var mu sync.Mutex
	var requestURIs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		// RequestURI is the raw URI from the request line. URL.Path cannot be
		// used here because the server decodes %2F back to a slash in it.
		requestURIs = append(requestURIs, r.RequestURI)
		mu.Unlock()
		name := filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/"))
		data, ok := files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()

	// Piece length is 16. fileA and fileB are aligned to piece boundaries
	// with padding files (BEP 47). fileB spans pieces 1 and 2.
	pieces := []piece.Piece{
		{Index: 0, Length: 16, Data: []filesection.FileSection{
			{Name: fileAName, Offset: 0, Length: 10},
			{Name: ".pad/6", Offset: 0, Length: 6, Padding: true},
		}},
		{Index: 1, Length: 16, Data: []filesection.FileSection{
			{Name: fileBName, Offset: 0, Length: 16},
		}},
		{Index: 2, Length: 16, Data: []filesection.FileSection{
			{Name: fileBName, Offset: 16, Length: 4},
			{Name: ".pad/12", Offset: 0, Length: 12, Padding: true},
		}},
		{Index: 3, Length: 4, Data: []filesection.FileSection{
			{Name: fileCName, Offset: 0, Length: 4},
		}},
	}
	concat := func(parts ...[]byte) []byte {
		var b []byte
		for _, p := range parts {
			b = append(b, p...)
		}
		return b
	}
	expected := [][]byte{
		concat(fileA, make([]byte, 6)),
		fileB[:16],
		concat(fileB[16:], make([]byte, 12)),
		fileC,
	}

	run := func(begin, end uint32) []*PieceResult {
		d := New(srv.URL, begin, end, nil)
		pool := bufferpool.New(16)
		resultC := make(chan *PieceResult)
		go d.Run(http.DefaultClient, pieces, true, resultC, pool, 5*time.Second)
		var results []*PieceResult
		for {
			select {
			case res := <-resultC:
				require.NoError(t, res.Error)
				results = append(results, res)
				if res.Done {
					d.Close()
					return results
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for piece result")
			}
		}
	}

	// Download all pieces.
	results := run(0, 4)
	require.Len(t, results, 4)
	for i, res := range results {
		assert.Equal(t, uint32(i), res.Index)
		assert.Equal(t, expected[i], res.Buffer.Data, "piece #%d", i)
		assert.Equal(t, i == 3, res.Done)
		res.Buffer.Release()
	}

	// Download a sub-range beginning at a piece that starts mid-file.
	results = run(2, 4)
	require.Len(t, results, 2)
	for i, res := range results {
		index := 2 + i
		assert.Equal(t, uint32(index), res.Index)
		assert.Equal(t, expected[index], res.Buffer.Data, "piece #%d", index)
		assert.Equal(t, index == 3, res.Done)
		res.Buffer.Release()
	}

	// Each file is requested once per run with its exact raw URI: path
	// separators as literal slashes (not %2F, see BEP 19), other special
	// characters escaped per segment, and no requests for padding files.
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{
		"/d/fileA", "/d/fileB", "/d/sub/file%20c", // run(0, 4)
		"/d/fileB", "/d/sub/file%20c", // run(2, 4)
	}, requestURIs)
}
