package announcer

import (
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
)

type Announcer struct {
	url        string
	transfer   tracker.Transfer
	log        logger.Logger
	completedC chan struct{}
	peerList   *peerlist.PeerList
}

func New(trackerURL string, to tracker.Transfer, completedC chan struct{}, pl *peerlist.PeerList, l logger.Logger) *Announcer {
	return &Announcer{
		url:        trackerURL,
		transfer:   to,
		log:        l,
		completedC: completedC,
		peerList:   pl,
	}
}

func (a *Announcer) Run(stopC chan struct{}) {
	u, err := url.Parse(a.url)
	if err != nil {
		a.log.Errorln("cannot parse tracker url:", err)
		return
	}
	var tr tracker.Tracker
	switch u.Scheme {
	case "http", "https":
		tr = httptracker.New(u)
	case "udp":
		tr = udptracker.New(u)
	default:
		a.log.Errorln("unsupported tracker scheme: %s", u.Scheme)
		return
	}

	var nextAnnounce time.Duration
	var m sync.Mutex

	retry := &backoff.ExponentialBackOff{
		InitialInterval:     5 * time.Second,
		RandomizationFactor: 0.5,
		Multiplier:          2,
		MaxInterval:         30 * time.Minute,
		MaxElapsedTime:      0, // never stop
		Clock:               backoff.SystemClock,
	}
	retry.Reset()

	announce := func(e tracker.Event) {
		m.Lock()
		defer m.Unlock()
		r, err := tr.Announce(a.transfer, e, stopC)
		if err != nil {
			a.log.Errorln("announce error:", err)
			nextAnnounce = retry.NextBackOff()
		} else {
			retry.Reset()
			nextAnnounce = r.Interval
			select {
			case a.peerList.NewPeers <- r.Peers:
			case <-stopC:
			}
		}
	}

	// Send start, stop and completed events.
	announce(tracker.EventStarted)
	defer announce(tracker.EventStopped)
	go func() {
		select {
		case <-a.completedC:
			announce(tracker.EventCompleted)
		case <-stopC:
		}
	}()

	// Send periodic announces.
	for {
		m.Lock()
		d := nextAnnounce
		m.Unlock()
		select {
		case <-time.After(d):
			announce(tracker.EventNone)
		case <-stopC:
			tr.Close()
			return
		}
	}
}
