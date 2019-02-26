package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
)

type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
}

func New(bl *blocklist.Blocklist) *TrackerManager {
	m := &TrackerManager{
		httpTransport: &http.Transport{
			// Setting TLSNextProto to non-nil map disables HTTP/2 support.
			TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
		},
		udpTransport: udptracker.NewTransport(bl),
	}
	m.httpTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		ip, port, err := tracker.ResolveHost(ctx, addr, bl)
		if err != nil {
			return nil, err
		}
		var d net.Dialer
		taddr := &net.TCPAddr{IP: ip, Port: port}
		return d.DialContext(ctx, network, taddr.String())
	}
	return m
}

func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		tr := httptracker.New(s, u, httpTimeout, m.httpTransport, httpUserAgent)
		return tr, nil
	case "udp":
		tr := udptracker.New(s, u, m.udpTransport)
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
