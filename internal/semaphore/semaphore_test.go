package semaphore

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestSemaphoreLen(t *testing.T) {
	s := New(3)
	assert.Equal(t, 0, s.Len())
	assert.Equal(t, 0, s.Waiting())

	s.Wait()
	s.Wait()
	assert.Equal(t, 2, s.Len())

	s.Wait()
	assert.Equal(t, 3, s.Len())

	s.Signal()
	assert.Equal(t, 2, s.Len())

	s.Signal()
	s.Signal()
	assert.Equal(t, 0, s.Len())
}

func TestSemaphoreBlocksWhenFull(t *testing.T) {
	s := New(1)
	s.Wait() // acquire the only slot
	require.Equal(t, 1, s.Len())

	var wg sync.WaitGroup
	wg.Add(1)
	acquired := make(chan struct{})
	go func() {
		defer wg.Done()
		s.Wait() // blocks until the slot is released
		close(acquired)
	}()

	// The goroutine is now blocked waiting on the semaphore.
	require.Eventually(t, func() bool {
		return s.Waiting() == 1
	}, time.Second, time.Millisecond)

	select {
	case <-acquired:
		t.Fatal("goroutine acquired the semaphore before it was released")
	default:
	}

	s.Signal() // release the slot; the waiting goroutine proceeds

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("goroutine did not acquire the semaphore after release")
	}

	wg.Wait()
	assert.Equal(t, 0, s.Waiting())

	s.Signal() // release the slot now held by the goroutine
	assert.Equal(t, 0, s.Len())
}
