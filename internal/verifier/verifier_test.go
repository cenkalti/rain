package verifier_test

import (
	"crypto/sha1"
	"io"
	"testing"

	"github.com/cenkalti/rain/v2/internal/filesection"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/cenkalti/rain/v2/internal/verifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memFile is an in-memory storage.File backing a piece's data.
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

func newPiece(index uint32, data, hash []byte) piece.Piece {
	mf := &memFile{b: append([]byte(nil), data...)}
	return piece.Piece{
		Index:  index,
		Length: uint32(len(data)),
		Data:   filesection.Piece{{File: mf, Offset: 0, Length: int64(len(data))}},
		Hash:   hash,
	}
}

func sha1Sum(b []byte) []byte {
	s := sha1.Sum(b)
	return s[:]
}

func TestVerifierMarksMatchingPieces(t *testing.T) {
	good := []byte("good piece data!") // 16 bytes
	bad := []byte("bad piece data!!")  // 16 bytes, stored with a wrong hash
	pieces := []piece.Piece{
		newPiece(0, good, sha1Sum(good)),
		newPiece(1, bad, sha1Sum([]byte("something else"))),
	}

	v := verifier.New()
	progressC := make(chan verifier.Progress, len(pieces))
	resultC := make(chan *verifier.Verifier, 1)
	go v.Run(pieces, progressC, resultC)

	res := <-resultC
	require.NoError(t, res.Error)
	assert.True(t, res.Bitfield.Test(0), "matching piece should be marked present")
	assert.False(t, res.Bitfield.Test(1), "mismatching piece should not be marked")

	// Progress was reported once per piece.
	assert.Equal(t, len(pieces), len(progressC))
}
