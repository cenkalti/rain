package resourcemanager

type ResourceManager struct {
	available int
	requests  map[string]request
	requestC  chan request
	releaseC  chan int
	closeC    chan struct{}
	doneC     chan struct{}
}

type request struct {
	key     string
	n       int
	notifyC chan struct{}
	closeC  chan struct{}
}

func New(limit int) *ResourceManager {
	m := &ResourceManager{
		available: limit,
		requests:  make(map[string]request),
		requestC:  make(chan request),
		releaseC:  make(chan int),
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
	go m.run()
	return m
}

func (m *ResourceManager) Close() {
	close(m.closeC)
	<-m.doneC
}

func (m *ResourceManager) Request(key string, n int, notifyC, closeC chan struct{}) {
	if n < 0 {
		return
	}
	r := request{
		key:     key,
		n:       n,
		notifyC: notifyC,
		closeC:  closeC,
	}
	select {
	case m.requestC <- r:
	case <-m.closeC:
	}
}

func (m *ResourceManager) Release(n int) {
	select {
	case m.releaseC <- n:
	case <-m.closeC:
	}
}

func (m *ResourceManager) run() {
	for {
		req := m.randomRequest()

		select {
		case r := <-m.requestC:
			m.requests[r.key] = r
		case n := <-m.releaseC:
			m.available += n
		case req.notifyC <- struct{}{}:
			m.available -= req.n
			delete(m.requests, req.key)
		case <-req.closeC:
			delete(m.requests, req.key)
		case <-m.closeC:
			close(m.doneC)
			return
		}
	}
}

func (m *ResourceManager) randomRequest() request {
	for _, r := range m.requests {
		if m.available >= r.n {
			return r
		}
	}
	return request{}
}
