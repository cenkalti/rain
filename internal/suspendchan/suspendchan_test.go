package suspendchan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChanSuspendResume(t *testing.T) {
	c := New[int](1)

	// While active, SendC and ReceiveC return the same underlying channel.
	require.NotNil(t, c.SendC())
	require.NotNil(t, c.ReceiveC())
	assert.Equal(t, c.SendC(), c.ReceiveC())

	// Suspend makes ReceiveC return nil (so a receiver blocks) while SendC
	// keeps returning the real channel.
	c.Suspend()
	assert.Nil(t, c.ReceiveC())
	assert.NotNil(t, c.SendC())

	// Resume reverts the suspend.
	c.Resume()
	assert.NotNil(t, c.ReceiveC())
	assert.Equal(t, c.SendC(), c.ReceiveC())
}

func TestChanSendReceive(t *testing.T) {
	c := New[string](1)
	c.SendC() <- "hello" // buffered, does not block
	assert.Equal(t, "hello", <-c.ReceiveC())
}
