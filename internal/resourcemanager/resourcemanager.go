package resourcemanager

type ResourceManager struct {
	limit     int64
	available int64
	requests  map[string]request
	requestC  chan request
	releaseC  chan int64
	statsC    chan chan Stats
	closeC    chan struct{}
	doneC     chan struct{}
}

type request struct {
	key     string
	n       int64
	notifyC chan struct{}
	cancelC chan struct{}
	doneC   chan bool
}

type Stats struct {
	Used  int64
	Count int
}

func New(limit int64) *ResourceManager {
	m := &ResourceManager{
		limit:     limit,
		available: limit,
		requests:  make(map[string]request),
		requestC:  make(chan request),
		releaseC:  make(chan int64),
		statsC:    make(chan chan Stats),
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

func (m *ResourceManager) Stats() Stats {
	var stats Stats
	ch := make(chan Stats)
	select {
	case m.statsC <- ch:
		select {
		case stats = <-ch:
		case <-m.closeC:
		}
	case <-m.closeC:
	}
	return stats
}

func (m *ResourceManager) Request(key string, n int64, notifyC, cancelC chan struct{}) (acquired bool) {
	if n < 0 {
		return
	}
	r := request{
		key:     key,
		n:       n,
		notifyC: notifyC,
		cancelC: cancelC,
		doneC:   make(chan bool),
	}
	select {
	case m.requestC <- r:
		select {
		case acquired = <-r.doneC:
		case <-m.closeC:
		}
	case <-m.closeC:
	}
	return
}

func (m *ResourceManager) Release(n int64) {
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
			m.handleRequest(r)
		case n := <-m.releaseC:
			m.available += n
		case req.notifyC <- struct{}{}:
			m.available -= req.n
			delete(m.requests, req.key)
		case <-req.cancelC:
			delete(m.requests, req.key)
		case ch := <-m.statsC:
			stats := Stats{
				Used:  m.limit - m.available,
				Count: len(m.requests),
			}
			select {
			case ch <- stats:
			case <-m.closeC:
			}
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

func (m *ResourceManager) handleRequest(r request) {
	acquired := m.available >= r.n
	select {
	case r.doneC <- acquired:
		if acquired {
			m.available -= r.n
		} else {
			m.requests[r.key] = r
		}
	case <-r.cancelC:
	}
}
