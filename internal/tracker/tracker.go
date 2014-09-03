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
	URL() string
	// Announce transfer to the tracker.
	Announce(t Transfer, e Event) (*AnnounceResponse, error)
	// Close must be called in order to close open connections if Announce is ever called.
	Close() error
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
		rawurl: trackerURL,
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

// Announce announces to the tracker periodically and adjust the interval according to the response returned by the tracker.
// Puts responses into responseC. Blocks when sending to this channel.
// t.Close must be called after using this function to close open connections to the tracker.
func Announce(t Tracker, transfer Transfer, cancel <-chan struct{}, startEvent Event, eventC <-chan Event, responseC chan<- *AnnounceResponse) {
	var nextAnnounce time.Duration
	var retry = *defaultRetryBackoff

	announce := func(e Event) {
		r, err := t.Announce(transfer, e)
		if err != nil {
			r = &AnnounceResponse{Error: err}
			nextAnnounce = retry.NextBackOff()
		} else {
			retry.Reset()
			nextAnnounce = r.Interval
		}
		select {
		case responseC <- r:
		case <-cancel:
			return
		}
	}

	announce(startEvent)
	for {
		select {
		case <-time.After(nextAnnounce):
			announce(None)
		case e := <-eventC:
			announce(e)
		case <-cancel:
			return
		}
	}
}

type trackerBase struct {
	url    *url.URL
	rawurl string
	peerID protocol.PeerID
	port   uint16
	log    logger.Logger
}

func (t trackerBase) URL() string { return t.rawurl }

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
