package trackermanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/rain/torrent/blocklist"
	"github.com/cenkalti/rain/tracker"
	"github.com/cenkalti/rain/tracker/httptracker"
	"github.com/cenkalti/rain/tracker/udptracker"
)

type TrackerManager struct {
	httpTransport *http.Transport
	udpTransports map[string]*udptracker.Transport
	blocklist     blocklist.Blocklist
	m             sync.Mutex
}

func New(bl blocklist.Blocklist) *TrackerManager {
	m := &TrackerManager{
		udpTransports: make(map[string]*udptracker.Transport),
		blocklist:     bl,
	}

	httpTransport := new(http.Transport)
	httpTransport.DialContext = m.dialContext

	m.httpTransport = httpTransport
	return m
}

func (m *TrackerManager) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var ips []net.IP
	ip := net.ParseIP(addr)
	if ip != nil {
		ips = append(ips, ip)
	} else {
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, addr)
		if err != nil {
			return nil, err
		}
		for _, ia := range addrs {
			ips = append(ips, ia.IP)
		}
	}
	for _, ip := range ips {
		if m.blocklist.Blocked(ip) {
			return nil, errors.New("ip is blocked")
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, ips[0].String())
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
