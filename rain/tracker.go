package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	Announce(t *transfer, cancel <-chan struct{}, event <-chan trackerEvent, peersC chan<- []peerAddr)
}

func (r *Rain) newTracker(trackerURL string) (tracker, error) {
	u, err := url.Parse(trackerURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		return r.newHTTPTracker(u), nil
	case "udp":
		return r.newUDPTracker(u), nil
	default:
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}
}

type trackerBase struct {
	URL    *url.URL
	peerID peerID
	port   uint16
	log    logger
}

func (r *Rain) newTrackerBase(u *url.URL) *trackerBase {
	return &trackerBase{
		URL:    u,
		peerID: r.peerID,
		port:   r.port(),
		log:    newLogger("tracker " + u.String()),
	}
}

func (t *trackerBase) parsePeers(r *bytes.Reader) ([]peerAddr, error) {
	t.log.Debugf("len(rest): %#v", r.Len())
	if r.Len()%6 != 0 {
		return nil, errors.New("invalid peer list")
	}

	count := r.Len() / 6
	t.log.Debugf("count of peers: %#v", count)
	peers := make([]peerAddr, count)
	for i := 0; i < count; i++ {
		if err := binary.Read(r, binary.BigEndian, &peers[i]); err != nil {
			return nil, err
		}
	}
	t.log.Debugf("peers: %#v\n", peers)

	return peers, nil
}

type trackerError string

func (e trackerError) Error() string { return string(e) }
