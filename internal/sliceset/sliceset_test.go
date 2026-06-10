package sliceset

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSliceSet(t *testing.T) {
	var s SliceSet[int]
	a, b, c := 1, 2, 3

	assert.Equal(t, 0, s.Len())
	assert.False(t, s.Has(&a))

	assert.True(t, s.Add(&a))
	assert.True(t, s.Add(&b))
	assert.True(t, s.Add(&c))
	assert.Equal(t, 3, s.Len())

	// Adding the same pointer again is a no-op.
	assert.False(t, s.Add(&a))
	assert.Equal(t, 3, s.Len())

	assert.True(t, s.Has(&a))
	assert.True(t, s.Has(&b))
	assert.True(t, s.Has(&c))

	// A distinct pointer with an equal value is not a member: membership is by
	// pointer identity, not value.
	other := 1
	assert.False(t, s.Has(&other))

	d := 4
	assert.False(t, s.Has(&d))

	// Removing a missing element returns false and leaves the set unchanged.
	assert.False(t, s.Remove(&d))
	assert.Equal(t, 3, s.Len())

	// Remove uses swap-with-last, so order is not preserved; assert membership.
	assert.True(t, s.Remove(&b))
	assert.Equal(t, 2, s.Len())
	assert.False(t, s.Has(&b))
	assert.True(t, s.Has(&a))
	assert.True(t, s.Has(&c))

	assert.True(t, s.Remove(&a))
	assert.True(t, s.Remove(&c))
	assert.Equal(t, 0, s.Len())
	assert.False(t, s.Has(&c))
}
