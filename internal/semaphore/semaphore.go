package semaphore

import "sync/atomic"

type Semaphore struct {
	c       chan token
	waiting int32
	active  int32
}

type token struct{}

func New(n int) *Semaphore {
	return &Semaphore{
		c: make(chan token, n),
	}
}

func (s *Semaphore) Waiting() int {
	return int(atomic.LoadInt32(&s.waiting))
}

func (s *Semaphore) Len() int {
	return int(atomic.LoadInt32(&s.active))
}

func (s *Semaphore) Wait() {
	atomic.AddInt32(&s.waiting, 1)
	s.c <- token{}
	atomic.AddInt32(&s.waiting, -1)
	atomic.AddInt32(&s.active, 1)
}

func (s *Semaphore) Signal() {
	<-s.c
	atomic.AddInt32(&s.active, -1)
}
