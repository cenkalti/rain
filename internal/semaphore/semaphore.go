package semaphore

type Semaphore struct {
	c chan token
}

type token struct{}

func New(n int) Semaphore {
	return Semaphore{
		c: make(chan token, n),
	}
}

func (s Semaphore) Wait() {
	s.c <- token{}
}

func (s Semaphore) Signal() {
	<-s.c
}
