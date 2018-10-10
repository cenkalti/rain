package semaphore

type Semaphore struct {
	Wait chan token
	n    int
}

type token struct{}

func New(n int) *Semaphore {
	return &Semaphore{
		Wait: make(chan token, n),
		n:    n,
	}
}

func (s *Semaphore) Start() {
	s.Signal(s.n)
}

func (s *Semaphore) Stop() {
	for {
		select {
		case <-s.Wait:
		default:
			return
		}
	}
}

func (s *Semaphore) Signal(n int) {
	for i := 0; i < n; i++ {
		select {
		case s.Wait <- token{}:
		default:
			return
		}
	}
}
