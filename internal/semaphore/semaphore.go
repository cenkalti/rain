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
	waiting int32
	active  int32
}

// New returns a new counting semaphore of length `n`.
func New(n int) *Semaphore {
	return &Semaphore{
		sem: semaphore.NewWeighted(int64(n)),
	}
}

// Waiting returs the number of waiting goroutines on the semaphore.
func (s *Semaphore) Waiting() int {
	return int(atomic.LoadInt32(&s.waiting))
}

// Len returns the number of goroutines currently acquired the semaphore.
func (s *Semaphore) Len() int {
	return int(atomic.LoadInt32(&s.active))
}

// Wait for the semaphore. Blocks until the resource is available.
// Returns an error if the context is cancelled before acquiring the semaphore.
func (s *Semaphore) Wait() error {
	atomic.AddInt32(&s.waiting, 1)
	err := s.sem.Acquire(context.TODO(), 1)
	atomic.AddInt32(&s.waiting, -1)
	if err != nil {
		return err
	}
	atomic.AddInt32(&s.active, 1)
	return nil
}

// Signal the semaphore. A random waiting goroutine will be waken up.
func (s *Semaphore) Signal() {
	s.sem.Release(1)
	atomic.AddInt32(&s.active, -1)
}
