package semaphore

import (
	"context"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

// Semaphore used to control access to a common resource by multiple goroutines.
// This is a wrapper around golang.org/x/sync/semaphore.Weighted with additional
// metrics tracking for waiting and active counts.
type Semaphore struct {
	sem     *semaphore.Weighted
	waiting atomic.Int32
	active  atomic.Int32
}

// New returns a new counting semaphore of length `n`.
func New(n int) *Semaphore {
	return &Semaphore{
		sem: semaphore.NewWeighted(int64(n)),
	}
}

// Waiting returs the number of waiting goroutines on the semaphore.
func (s *Semaphore) Waiting() int {
	return int(s.waiting.Load())
}

// Len returns the number of goroutines currently acquired the semaphore.
func (s *Semaphore) Len() int {
	return int(s.active.Load())
}

// Wait for the semaphore. Blocks until the resource is available.
// Returns an error if the context is cancelled before acquiring the semaphore.
func (s *Semaphore) Wait() {
	s.waiting.Add(1)
	_ = s.sem.Acquire(context.TODO(), 1)
	s.waiting.Add(-1)
	s.active.Add(1)
}

// Signal the semaphore. A random waiting goroutine will be waken up.
func (s *Semaphore) Signal() {
	s.sem.Release(1)
	s.active.Add(-1)
}
