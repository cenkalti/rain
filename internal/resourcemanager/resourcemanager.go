package resourcemanager

import "math/rand"

// ResourceManager is a fair manager for distributing limited amount of resources to requesters.
type ResourceManager[T any] struct {
	limit     int64
	available int64
	objects   int
	requests  map[string][]request[T]
	requestC  chan request[T]
	releaseC  chan int64
	statsC    chan chan Stats
	closeC    chan struct{}
	doneC     chan struct{}
}

type request[T any] struct {
	key     string
	data    T
	n       int64
	notifyC chan T
	cancelC chan struct{}
	doneC   chan bool
}

// Stats about ResourceManager
type Stats struct {
	AllocatedSize    int64
	AllocatedObjects int
	PendingKeys      int
}

// New returns a new ResourceManager with `limit` number of resources.
func New[T any](limit int64) *ResourceManager[T] {
	m := &ResourceManager[T]{
		limit:     limit,
		available: limit,
		requests:  make(map[string][]request[T]),
		requestC:  make(chan request[T]),
		releaseC:  make(chan int64),
		statsC:    make(chan chan Stats),
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
	go m.run()
	return m
}

// Close resource manager.
func (m *ResourceManager[T]) Close() {
	close(m.closeC)
	<-m.doneC
}

// Stats returns statistics about current status.
func (m *ResourceManager[T]) Stats() Stats {
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

// Request `n` resource from the manager for key `key`.
// Release must be called after done with the resource.
func (m *ResourceManager[T]) Request(key string, data T, n int64, notifyC chan T, cancelC chan struct{}) (acquired bool) {
	if n < 0 {
		return
	}
	r := request[T]{
		key:     key,
		data:    data,
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

// Release `n` resource to the manager.
func (m *ResourceManager[T]) Release(n int64) {
	select {
	case m.releaseC <- n:
	case <-m.closeC:
	}
}

func (m *ResourceManager[T]) run() {
	for {
		req, i := m.randomRequest()
		select {
		case r := <-m.requestC:
			m.handleRequest(r)
		case n := <-m.releaseC:
			m.available += n
			m.objects--
			if m.available > m.limit {
				panic("invalid release call")
			}
		case req.notifyC <- req.data:
			m.available -= req.n
			m.objects++
			if m.available < 0 {
				panic("invalid request call 1")
			}
			m.deleteRequest(req.key, i)
		case <-req.cancelC:
			m.deleteRequest(req.key, i)
		case ch := <-m.statsC:
			stats := Stats{
				AllocatedObjects: m.objects,
				AllocatedSize:    m.limit - m.available,
				PendingKeys:      len(m.requests),
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

func (m *ResourceManager[T]) deleteRequest(key string, i int) {
	rs := m.requests[key]
	rs[i] = rs[len(rs)-1]
	rs = rs[:len(rs)-1]
	if len(rs) > 0 {
		m.requests[key] = rs
	} else {
		delete(m.requests, key)
	}
}

func (m *ResourceManager[T]) randomRequest() (request[T], int) {
	for _, rs := range m.requests {
		i := rand.Intn(len(rs)) // nolint: gosec
		r := rs[i]
		if r.n > m.available {
			break
		}
		return r, i
	}
	return request[T]{}, -1
}

func (m *ResourceManager[T]) handleRequest(r request[T]) {
	acquired := m.available >= r.n
	select {
	case r.doneC <- acquired:
		if acquired {
			m.available -= r.n
			m.objects++
			if m.available < 0 {
				panic("invalid request call 2")
			}
		} else {
			m.requests[r.key] = append(m.requests[r.key], r)
		}
	case <-r.cancelC:
	}
}
