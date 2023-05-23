package resourcemanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResourceManager(t *testing.T) {
	m := New[string](2)
	require.Zero(t, m.Stats().AllocatedObjects)
	ok := m.Request("foo", "", 1, nil, nil)
	require.True(t, ok)
	require.Equal(t, 1, m.Stats().AllocatedObjects)
	ok = m.Request("foo", "", 1, nil, nil)
	require.True(t, ok)
	require.Equal(t, 2, m.Stats().AllocatedObjects)
	notifyC := make(chan string)
	ok = m.Request("foo", "bar", 1, notifyC, nil)
	require.False(t, ok)
	require.Equal(t, 2, m.Stats().AllocatedObjects)
	m.Release(1)
	require.Equal(t, 1, m.Stats().AllocatedObjects)
	var data string
	select {
	case data = <-notifyC:
	case <-time.After(time.Second):
		t.Fatal("notify channel did not get the message")
	}
	require.Equal(t, "bar", data)
	require.Equal(t, 2, m.Stats().AllocatedObjects)
}
