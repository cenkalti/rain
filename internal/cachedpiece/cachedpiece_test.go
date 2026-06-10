package cachedpiece_test

import (
	"io"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/cachedpiece"
	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/cenkalti/rain/v2/internal/piececache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memFile struct{ b []byte }

func (m *memFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(m.b)) {
		return 0, io.EOF
	}
	n := copy(p, m.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *memFile) WriteAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > int64(len(m.b)) {
		return 0, io.ErrShortWrite
	}
	return copy(m.b[off:], p), nil
}

func TestCachedPieceReadAt(t *testing.T) {
	data := []byte("0123456789ABCDEF") // 16 bytes
	mf := &memFile{b: append([]byte(nil), data...)}
	p := piece.Piece{
		Index:  0,
		Length: uint32(len(data)),
		Data:   filesection.Piece{{File: mf, Offset: 0, Length: int64(len(data))}},
	}

	cache := piececache.New(1<<20, time.Minute, 1)
	t.Cleanup(cache.Close)

	const readSize = 8
	cp := cachedpiece.New(&p, cache, readSize, [20]byte{1})

	// Read within the first block.
	buf := make([]byte, 4)
	n, err := cp.ReadAt(buf, 2)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, data[2:6], buf)

	// Read within the second block.
	buf2 := make([]byte, 4)
	n, err = cp.ReadAt(buf2, 8)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, data[8:12], buf2)

	// A repeated read is served from the cache and returns the same bytes.
	buf3 := make([]byte, 4)
	_, err = cp.ReadAt(buf3, 2)
	require.NoError(t, err)
	assert.Equal(t, data[2:6], buf3)
}
