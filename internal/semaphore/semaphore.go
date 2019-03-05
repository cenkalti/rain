package semaphore

import "sync/atomic"

type Semaphore struct {
	c       chan token
	waiting int32
}

type token struct{}

func New(n int) *Semaphore {
	return &Semaphore{
		c: make(chan token, n),
	}
}

func (s *Semaphore) Waiting() int32 {
	return atomic.LoadInt32(&s.waiting)
}

func (s *Semaphore) Wait() {
	atomic.AddInt32(&s.waiting, 1)
	s.c <- token{}
	atomic.AddInt32(&s.waiting, -1)
}

func (s *Semaphore) Signal() {
	<-s.c
}
