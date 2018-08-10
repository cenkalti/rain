package torrent

import (
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/tracker"
)

func (t *Torrent) announcer() {
	defer t.stopWG.Done()
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

	// Send start, stop and completed events.
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

	// Send periodic announces.
	for {
		m.Lock()
		d := nextAnnounce
		m.Unlock()
		select {
		case <-time.After(d):
			announce(tracker.EventNone)
		case <-t.stopC:
			// Wake up dialer goroutine waiting for new peers because we won't get more peers.
			t.gotPeer.Broadcast()
			return
		}
	}
}
