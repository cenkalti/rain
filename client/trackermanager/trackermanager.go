package trackermanager

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cenkalti/rain/torrent/blocklist"
	"github.com/cenkalti/rain/tracker"
	"github.com/cenkalti/rain/tracker/httptracker"
	"github.com/cenkalti/rain/tracker/udptracker"
)

type TrackerManager struct {
	httpTransport *http.Transport
	udpTransport  *udptracker.Transport
	blocklist     blocklist.Blocklist
}

func New(bl blocklist.Blocklist) *TrackerManager {
	m := &TrackerManager{
		blocklist:    bl,
		udpTransport: udptracker.NewTransport(bl),
	}

	httpTransport := new(http.Transport)
	httpTransport.DialContext = m.dialContext

	m.httpTransport = httpTransport
	return m
}

func (m *TrackerManager) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	ip, port, err := tracker.ResolveHost(ctx, addr, m.blocklist)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	taddr := &net.TCPAddr{IP: ip, Port: port}
	return d.DialContext(ctx, network, taddr.String())
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
