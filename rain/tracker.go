package rain

import (
	"fmt"
	"net/url"
)

// Number of peers we want from trackers
const numWant = 50

type trackerEvent int32

// Tracker Announce Events
const (
	trackerEventNone trackerEvent = iota
	trackerEventCompleted
	trackerEventStarted
	trackerEventStopped
)

type tracker interface {
	Announce(t *transfer, cancel <-chan struct{}, event <-chan trackerEvent, responseC chan<- *announceResponse)
}

type announceResponse struct {
	announceResponseBase
	Peers []peerAddr
}

func (r *Rain) newTracker(trackerURL string) (tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "udp":
		return r.newUDPTracker(u), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

type trackerError string

func (e trackerError) Error() string { return string(e) }
