package piecewriter_test

import (
	"bytes"
	"crypto/sha1"
	"io"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/bufferpool"
	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/cenkalti/rain/v2/internal/piecewriter"
	"github.com/cenkalti/rain/v2/internal/semaphore"
	metrics "github.com/rcrowley/go-metrics"
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

func newWriter(t *testing.T, data, hash []byte, dst *memFile) *piecewriter.PieceWriter {
	t.Helper()
	p := piece.Piece{
		Index:  0,
		Length: uint32(len(data)),
		Data:   filesection.Piece{{File: dst, Offset: 0, Length: int64(len(data))}},
		Hash:   hash,
	}
	buf := bufferpool.New(len(data)).Get(len(data))
	copy(buf.Data, data)
	return piecewriter.New(&p, "source", buf)
}

func run(t *testing.T, w *piecewriter.PieceWriter) *piecewriter.PieceWriter {
	t.Helper()
	resultC := make(chan *piecewriter.PieceWriter, 1)
	sem := semaphore.New(1)
	go w.Run(resultC, make(chan struct{}), metrics.NilMeter{}, metrics.NilMeter{}, sem)
	select {
	case res := <-resultC:
		return res
	case <-time.After(5 * time.Second):
		t.Fatal("piece writer did not finish")
		return nil
	}
}

func TestPieceWriterWritesOnHashMatch(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 64)
	sum := sha1.Sum(data)
	dst := &memFile{b: make([]byte, len(data))}

	res := run(t, newWriter(t, data, sum[:], dst))
	assert.True(t, res.HashOK)
	require.NoError(t, res.Error)
	assert.Equal(t, data, dst.b)
}

func TestPieceWriterSkipsWriteOnHashMismatch(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 64)
	dst := &memFile{b: make([]byte, len(data))}

	res := run(t, newWriter(t, data, make([]byte, 20), dst))
	assert.False(t, res.HashOK)
	require.NoError(t, res.Error)
	assert.Equal(t, make([]byte, len(data)), dst.b, "no bytes should be written on hash mismatch")
}
