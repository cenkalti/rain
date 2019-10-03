package semaphore

import "sync/atomic"

// Semaphore used to control access to a common resource by multiple goroutines.
type Semaphore struct {
	c       chan token
	waiting int32
	active  int32
}

type token struct{}

// New returns a new counting semaphore of length `n`.
func New(n int) *Semaphore {
	return &Semaphore{
		c: make(chan token, n),
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
func (s *Semaphore) Wait() {
	atomic.AddInt32(&s.waiting, 1)
	s.c <- token{}
	atomic.AddInt32(&s.waiting, -1)
	atomic.AddInt32(&s.active, 1)
}

// Signal the semaphore. A random waiting goroutine will be waken up.
func (s *Semaphore) Signal() {
	<-s.c
	atomic.AddInt32(&s.active, -1)
}
