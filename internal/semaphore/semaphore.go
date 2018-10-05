package semaphore

type Semaphore struct {
	Ready chan token
	n     int
}

type token struct{}

func New(n int) *Semaphore {
	return &Semaphore{
		Ready: make(chan token, n),
		n:     n,
	}
}

func (s *Semaphore) Start() {
	s.Signal(s.n)
}

func (s *Semaphore) Stop() {
	for {
		select {
		case <-s.Ready:
		default:
			return
		}
	}
}

func (s *Semaphore) Signal(n int) {
	for i := 0; i < n; i++ {
		select {
		case s.Ready <- token{}:
		default:
			return
		}
	}
}
