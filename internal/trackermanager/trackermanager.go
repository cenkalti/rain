package trackermanager

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cenkalti/rain/v2/internal/blocklist"
	"github.com/cenkalti/rain/v2/internal/resolver"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/cenkalti/rain/v2/internal/tracker/httptracker"
	"github.com/cenkalti/rain/v2/internal/tracker/udptracker"
)

// DialFunc is a function that dials a network connection, matching net.Dialer.DialContext signature.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// TrackerManager is a manager for using the same transport for same domains/IPs.
// Manages both HTTP and UDP trackers.
type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
}

// New returns a new TrackerManager.
func New(bl *blocklist.Blocklist, dnsTimeout time.Duration, tlsSkipVerify bool, customDial DialFunc) *TrackerManager {
	m := &TrackerManager{
		httpTransport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsSkipVerify}, // nolint: gosec
		},
		udpTransport: udptracker.NewTransport(bl, dnsTimeout),
	}
	go m.udpTransport.Run()
	m.httpTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		ip, port, err := resolver.Resolve(ctx, addr, dnsTimeout, bl)
		if err != nil {
			return nil, err
		}
		taddr := &net.TCPAddr{IP: ip, Port: port}
		if customDial != nil {
			return customDial(ctx, network, taddr.String())
		}
		var d net.Dialer
		return d.DialContext(ctx, network, taddr.String())
	}
	return m
}

func (m *TrackerManager) Close() {
	m.httpTransport.CloseIdleConnections()
	m.udpTransport.Close()
}

// Get a new Tracker implementation from the manager.
func (m *TrackerManager) Get(s string, httpTimeout time.Duration, httpUserAgent string, httpMaxResponseLength int64) (tracker.Tracker, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		tr := httptracker.New(s, u, httpTimeout, m.httpTransport, httpUserAgent, httpMaxResponseLength)
		return tr, nil
	case "udp":
		tr := udptracker.New(s, u, m.udpTransport)
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}
