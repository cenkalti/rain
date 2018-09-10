package semaphore

type Semaphore struct {
	Wait chan struct{}
}

func New(n int) *Semaphore {
	return &Semaphore{
		Wait: make(chan struct{}, n),
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
		case s.Wait <- struct{}{}:
		default:
			return
		}
	}
}
