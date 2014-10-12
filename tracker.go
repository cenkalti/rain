// Provides support for announcing torrents to HTTP and UDP trackers.

package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/cenkalti/backoff"
)

// Number of peers we want from trackers
const numWant = 50

var requestCancelledErr = errors.New("request cancelled")

type tracker interface {
	URL() string
	// Announce transfer to the tracker.
	Announce(t Transfer, e trackerEvent, cancel <-chan struct{}) (*announceResponse, error)
	Scrape([]Transfer) (*scrapeResponse, error)
	// Close must be called in order to close open connections if Announce is ever called.
	Close() error
}

type Transfer interface {
	InfoHash() InfoHash
	Downloaded() int64
	Uploaded() int64
	Left() int64
}

type announceResponse struct {
	Error      error
	Interval   time.Duration
	Leechers   int32
	Seeders    int32
	Peers      []*net.TCPAddr
	ExternalIP net.IP
}

type scrapeResponse struct {
	// TODO not implemented
}

func newTracker(trackerURL string, c client) (tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	base := &trackerBase{
		url:    u,
		rawurl: trackerURL,
		peerID: c.PeerID(),
		port:   c.Port(),
		log:    NewLogger("tracker " + trackerURL),
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

// AnnouncePeriodically announces to the tracker periodically and adjust the interval according to the response returned by the tracker.
// Puts responses into responseC. Blocks when sending to this channel.
// t.Close must be called after using this function to close open connections to the tracker.
func announcePeriodically(t tracker, transfer Transfer, cancel <-chan struct{}, startEvent trackerEvent, eventC <-chan trackerEvent, responseC chan<- *announceResponse) {
	var nextAnnounce time.Duration
	var retry = *defaultRetryBackoff

	announce := func(e trackerEvent) {
		r, err := t.Announce(transfer, e, cancel)
		if err != nil {
			r = &announceResponse{Error: err}
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
	peerID PeerID
	port   uint16
	log    Logger
}

func (t trackerBase) URL() string { return t.rawurl }

type client interface {
	PeerID() PeerID
	Port() uint16
}

// parsePeersBinary parses compact representation of peer list.
func (t *trackerBase) parsePeersBinary(r *bytes.Reader) ([]*net.TCPAddr, error) {
	t.log.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		b := make([]byte, r.Len())
		r.Read(b)
		t.log.Debugf("Peers: %q", b)
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	t.log.Debugf("count of peers: %#v", count)
	peers := make([]*net.TCPAddr, count)
	for i := 0; i < count; i++ {
		var peer struct {
			IP   [net.IPv4len]byte
			Port uint16
		}
		if err := binary.Read(r, binary.BigEndian, &peer); err != nil {
			return nil, err
		}
		peers[i] = &net.TCPAddr{IP: peer.IP[:], Port: int(peer.Port)}
	}

	t.log.Debugf("peers: %#v\n", peers)
	return peers, nil
}

// Error is the string that is sent by the tracker from announce or scrape.
type trackerError string

func (e trackerError) Error() string { return string(e) }

type trackerEvent int32

// Tracker Announce Events. Numbers corresponds to constants in UDP tracker protocol.
const (
	None trackerEvent = iota
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
func (e trackerEvent) String() string {
	return eventNames[e]
}

// defaultRetryBackoff is the back-off algorithm to use before retrying failed announce and scrape operations.
var defaultRetryBackoff = &backoff.ExponentialBackOff{
	InitialInterval:     5 * time.Second,
	RandomizationFactor: 0.5,
	Multiplier:          2,
	MaxInterval:         30 * time.Minute,
	MaxElapsedTime:      0, // never stop
	Clock:               backoff.SystemClock,
}

func init() {
	defaultRetryBackoff.Reset()
}
