package trackermanager

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/httptracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/udptracker"
)

type TrackerManager struct {
	httpHostCount  map[string]int
	udpHostCount   map[string]int
	httpTransports map[string]*http.Transport
	udpTransports  map[string]*udptracker.Transport
	httpHosts      map[*httptracker.HTTPTracker]string
	udpHosts       map[*udptracker.UDPTracker]string
	m              sync.Mutex
}

var DefaultTrackerManager = New()

func New() *TrackerManager {
	return &TrackerManager{
		httpHostCount:  make(map[string]int),
		udpHostCount:   make(map[string]int),
		httpTransports: make(map[string]*http.Transport),
		udpTransports:  make(map[string]*udptracker.Transport),
		httpHosts:      make(map[*httptracker.HTTPTracker]string),
		udpHosts:       make(map[*udptracker.UDPTracker]string),
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
		t := m.httpTransports[u.Host]
		if t == nil {
			t = new(http.Transport)
			m.httpTransports[u.Host] = t
		}
		m.httpHostCount[u.Host]++
		tr := httptracker.New(s, u, httpTimeout, t, httpUserAgent)
		m.httpHosts[tr] = u.Host
		return tr, nil
	case "udp":
		t := m.udpTransports[u.Host]
		if t == nil {
			t = udptracker.NewTransport(u.Host)
			m.udpTransports[u.Host] = t
		}
		m.udpHostCount[u.Host]++
		tr := udptracker.New(s, u.RequestURI(), t)
		m.udpHosts[tr] = u.Host
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

func (m *TrackerManager) Release(trk tracker.Tracker) {
	m.m.Lock()
	defer m.m.Unlock()
	switch t := trk.(type) {
	case *httptracker.HTTPTracker:
		host := m.httpHosts[t]
		delete(m.httpHosts, t)
		m.httpHostCount[host]--
		if m.httpHostCount[host] == 0 {
			tr := m.httpTransports[host]
			tr.CloseIdleConnections()
			delete(m.httpHostCount, host)
			delete(m.httpTransports, host)
		}
	case *udptracker.UDPTracker:
		host := m.udpHosts[t]
		delete(m.udpHosts, t)
		m.udpHostCount[host]--
		if m.udpHostCount[host] == 0 {
			tr := m.udpTransports[host]
			tr.Close()
			delete(m.udpHostCount, host)
			delete(m.udpTransports, host)
		}
	default:
		panic("unknown tracker type")
	}
}
