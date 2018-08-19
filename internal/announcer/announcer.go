package announcer

import (
	"net/url"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerlist"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
)

const stopEventTimeout = time.Minute

type Announcer struct {
	url          string
	transfer     tracker.Transfer
	log          logger.Logger
	completedC   chan struct{}
	peerList     *peerlist.PeerList
	tracker      tracker.Tracker
	backoff      backoff.BackOff
	nextAnnounce time.Duration
}

func New(trackerURL string, to tracker.Transfer, completedC chan struct{}, pl *peerlist.PeerList, l logger.Logger) *Announcer {
	return &Announcer{
		url:        trackerURL,
		transfer:   to,
		log:        l,
		completedC: completedC,
		peerList:   pl,
		backoff: &backoff.ExponentialBackOff{
			InitialInterval:     5 * time.Second,
			RandomizationFactor: 0.5,
			Multiplier:          2,
			MaxInterval:         30 * time.Minute,
			MaxElapsedTime:      0, // never stop
			Clock:               backoff.SystemClock,
		},
	}
}

func (a *Announcer) Run(stopC chan struct{}) {
	u, err := url.Parse(a.url)
	if err != nil {
		a.log.Errorln("cannot parse tracker url:", err)
		return
	}
	switch u.Scheme {
	case "http", "https":
		a.tracker = httptracker.New(u)
	case "udp":
		a.tracker = udptracker.New(u)
	default:
		a.log.Errorln("unsupported tracker scheme: %s", u.Scheme)
		return
	}

	a.backoff.Reset()
	a.announce(tracker.EventStarted, stopC)
	for {
		select {
		case <-time.After(a.nextAnnounce):
			a.announce(tracker.EventNone, stopC)
		case <-a.completedC:
			a.announce(tracker.EventCompleted, stopC)
			a.completedC = nil
		case <-stopC:
			go a.announceStopAndClose()
			return
		}
	}
}

func (a *Announcer) announce(e tracker.Event, stopC chan struct{}) {
	r, err := a.tracker.Announce(a.transfer, e, stopC)
	if err != nil {
		a.log.Errorln("announce error:", err)
		a.nextAnnounce = a.backoff.NextBackOff()
	} else {
		a.backoff.Reset()
		a.nextAnnounce = r.Interval
		select {
		case a.peerList.NewPeers <- r.Peers:
		case <-stopC:
		}
	}
}

func (a *Announcer) announceStopAndClose() {
	stopC := make(chan struct{})
	go func() {
		<-time.After(stopEventTimeout)
		close(stopC)
	}()
	a.announce(tracker.EventStopped, stopC)
	a.tracker.Close()
}
