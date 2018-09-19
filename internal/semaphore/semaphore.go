package semaphore

type Semaphore struct {
	Wait chan token
}

type token struct{}

func New(n int) *Semaphore {
	ch := make(chan token, n)
	for i := 0; i < n; i++ {
		ch <- token{}
	}
	return &Semaphore{
		Wait: ch,
	}
}

func (s *Semaphore) Block() {
	for {
		select {
		case <-s.Wait:
		default:
			return
		}
	}
}

func (s *Semaphore) Signal(n uint32) {
	for i := uint32(0); i < n; i++ {
		select {
		case s.Wait <- token{}:
		default:
			return
		}
	}
}
