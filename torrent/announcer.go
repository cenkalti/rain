package torrent

import (
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/tracker"
)

// TODO implement
func (t *Torrent) announcer() {
	defer t.stopWG.Done()
	var nextAnnounce time.Duration

	retry := &backoff.ExponentialBackOff{
		InitialInterval:     5 * time.Second,
		RandomizationFactor: 0.5,
		Multiplier:          2,
		MaxInterval:         30 * time.Minute,
		MaxElapsedTime:      0, // never stop
		Clock:               backoff.SystemClock,
	}
	retry.Reset()

	var m sync.Mutex
	announce := func(e tracker.Event) {
		m.Lock()
		defer m.Unlock()
		r, err := t.tracker.Announce(t, e, t.stopC)
		if err != nil {
			t.log.Errorln("announce error:", err)
			nextAnnounce = retry.NextBackOff()
		} else {
			retry.Reset()
			nextAnnounce = r.Interval
			t.putPeerAddrs(r.Peers)
		}
	}

	announce(tracker.EventStarted)
	defer announce(tracker.EventStopped)
	go func() {
		select {
		case <-t.completed:
			announce(tracker.EventCompleted)
		case <-t.stopC:
			return
		}

	}()

	for {
		select {
		case <-time.After(nextAnnounce):
			announce(tracker.EventNone)
		case <-t.stopC:
			return
		}
	}
}
