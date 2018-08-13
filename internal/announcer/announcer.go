package announcer

import (
	"sync"
	"time"

	"github.com/cenkalti/backoff"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
)

type Announcer struct {
	Tracker      tracker.Tracker
	Transfer     tracker.Transfer
	Log          logger.Logger
	peerAddrs    []*peerAddr          // contains peers not connected yet, sorted by oldest first
	peerAddrsMap map[string]*peerAddr // contains peers not connected yet, keyed by addr string
	gotPeer      *sync.Cond           // for waking announcer when got new peers from tracker
	completedC   chan struct{}
	m            sync.Mutex
	done         bool
}

func New(tr tracker.Tracker, to tracker.Transfer, completedC chan struct{}, l logger.Logger) *Announcer {
	a := &Announcer{
		Tracker:      tr,
		Transfer:     to,
		Log:          l,
		completedC:   completedC,
		peerAddrsMap: make(map[string]*peerAddr),
	}
	a.gotPeer = sync.NewCond(&a.m)
	return a
}

func (a *Announcer) Run(stopC chan struct{}) {
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
		r, err := a.Tracker.Announce(a.Transfer, e, stopC)
		if err != nil {
			a.Log.Errorln("announce error:", err)
			nextAnnounce = retry.NextBackOff()
		} else {
			retry.Reset()
			nextAnnounce = r.Interval
			a.putPeerAddrs(r.Peers)
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
			return
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
			// Wake up dialer goroutine waiting for new peers because we won't get more peers.
			a.done = true
			a.gotPeer.Broadcast()
			return
		}
	}
}
