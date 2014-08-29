// Package tracker implements HTTP and UDP tracker clients.
package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/backoff"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
)

// Number of peers we want from trackers
const NumWant = 50

type Tracker interface {
	// Announce is called in a go statement.
	// It announces to the tracker periodically and adjust the interval according to the response
	// returned by the tracker.
	// Puts the responses into r. Blocks when sending to this channel.
	Announce(t Transfer, cancel <-chan struct{}, e <-chan Event, r chan<- *AnnounceResponse)
}

type Transfer interface {
	InfoHash() protocol.InfoHash
	Downloaded() int64
	Uploaded() int64
	Left() int64
}

type AnnounceResponse struct {
	Error    error
	Interval time.Duration
	Leechers int32
	Seeders  int32
	Peers    []Peer
}

type Peer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

func (p Peer) TCPAddr() *net.TCPAddr {
	ip := make(net.IP, net.IPv4len)
	copy(ip, p.IP[:])
	return &net.TCPAddr{
		IP:   ip,
		Port: int(p.Port),
	}
}

func New(trackerURL string, c Client) (Tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	base := &trackerBase{
		url:    u,
		peerID: c.PeerID(),
		port:   c.Port(),
		log:    logger.New("tracker " + trackerURL),
	}

	switch u.Scheme {
	case "http", "https":
		return newHTTPTracker(base), nil
	case "udp":
		return newUDPTracker(base), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

type trackerBase struct {
	url    *url.URL
	peerID protocol.PeerID
	port   uint16
	log    logger.Logger
}

type Client interface {
	PeerID() protocol.PeerID
	Port() uint16
}

// parsePeers parses compact representation of peer list.
func (t *trackerBase) parsePeers(r *bytes.Reader) ([]Peer, error) {
	t.log.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	t.log.Debugf("count of peers: %#v", count)
	peers := make([]Peer, count)
	for i := 0; i < count; i++ {
		if err := binary.Read(r, binary.BigEndian, &peers[i]); err != nil {
			return nil, err
		}
	}
	t.log.Debugf("peers: %#v\n", peers)

	return peers, nil
}

// Error is the string that is sent by the tracker from announce or scrape.
type Error string

func (e Error) Error() string { return string(e) }

type Event int32

// Tracker Announce Events. Numbers corresponds to constants in UDP tracker protocol.
const (
	None Event = iota
	Completed
	Started
	Stopped
)

var eventNames = [...]string{
	"empty",
	"completed",
	"started",
	"stopped",
}

// String returns the name of event as represented in HTTP tracker protocol.
func (e Event) String() string {
	return eventNames[e]
}

// defaultRetryBackoff is the back-off algorithm to use before retrying failed announce and scrape operations.
var defaultRetryBackoff = &backoff.ExponentialBackOff{
	InitialInterval:     5 * time.Second,
	RandomizationFactor: 0.5,
	Multiplier:          2,
	MaxInterval:         30 * time.Minute,
	MaxElapsedTime:      1<<63 - 1, // max duration (~290 years)
	Clock:               backoff.SystemClock,
}

func init() {
	defaultRetryBackoff.Reset()
}
