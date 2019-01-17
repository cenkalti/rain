package trackermanager

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/rain/tracker"
	"github.com/cenkalti/rain/tracker/httptracker"
	"github.com/cenkalti/rain/tracker/udptracker"
)

type TrackerManager struct {
	httpTransport *http.Transport
	udpTransports map[string]*udptracker.Transport
	m             sync.Mutex
}

func New() *TrackerManager {
	httpTransport := new(http.Transport)
	return &TrackerManager{
		httpTransport: httpTransport,
		udpTransports: make(map[string]*udptracker.Transport),
	}
}

func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	m.m.Lock()
	defer m.m.Unlock()
	switch u.Scheme {
	case "http", "https":
		tr := httptracker.New(s, u, httpTimeout, m.httpTransport, httpUserAgent)
		return tr, nil
	case "udp":
		t := m.udpTransports[u.Host]
		if t == nil {
			t = udptracker.NewTransport(u.Host)
			m.udpTransports[u.Host] = t
		}
		tr := udptracker.New(s, u.RequestURI(), t)
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
