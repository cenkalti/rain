package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaddingFileReadAtServesZeroes(t *testing.T) {
	f := newPaddingFile()
	buf := []byte{1, 2, 3, 4}
	n, err := f.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []byte{0, 0, 0, 0}, buf)
}

func TestPaddingFileWriteAtPanics(t *testing.T) {
	f := newPaddingFile()
	assert.Panics(t, func() { _, _ = f.WriteAt([]byte{1}, 0) })
}

func TestPaddingFileClose(t *testing.T) {
	assert.NoError(t, newPaddingFile().Close())
}
