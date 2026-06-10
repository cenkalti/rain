package bufferpool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolGetLength(t *testing.T) {
	const bufLen = 16
	p := New(bufLen)

	b := p.Get(8)
	require.Len(t, b.Data, 8)

	// Data is zeroed on Get.
	for _, x := range b.Data {
		require.Zero(t, x)
	}

	full := p.Get(bufLen)
	require.Len(t, full.Data, bufLen)

	full.Release()
	b.Release()
}

func TestPoolClearsOnReuse(t *testing.T) {
	const bufLen = 16
	p := New(bufLen)

	// Dirty a buffer and return it to the pool.
	b := p.Get(bufLen)
	for i := range b.Data {
		b.Data[i] = 0xff
	}
	b.Release()

	// A subsequent Get must hand back zeroed Data even if the underlying array
	// is reused from the pool (sync.Pool does not guarantee reuse, so this
	// holds either way).
	b2 := p.Get(bufLen)
	for _, x := range b2.Data {
		assert.Zero(t, x)
	}
	b2.Release()
}
