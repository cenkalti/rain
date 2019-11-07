package announcer

import (
	"time"

	"github.com/cenkalti/rain/internal/logger"
)

// DHTAnnouncer runs a function periodically to announce the Torrent to DHT network.
type DHTAnnouncer struct {
	lastAnnounce   time.Time
	needMorePeers  bool
	needMorePeersC chan bool
	closeC         chan struct{}
	doneC          chan struct{}
}

// NewDHTAnnouncer returns a new DHTAnnouncer.
func NewDHTAnnouncer() *DHTAnnouncer {
	return &DHTAnnouncer{
		needMorePeersC: make(chan bool),
		closeC:         make(chan struct{}),
		doneC:          make(chan struct{}),
		needMorePeers:  true,
	}
}

// Close the announcer.
func (a *DHTAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

// NeedMorePeers signals the announcer goroutine to fetch more peers from DHT.
func (a *DHTAnnouncer) NeedMorePeers(val bool) {
	select {
	case a.needMorePeersC <- val:
	case <-a.doneC:
	}
}

// Run the announcer. Invoke with go statement.
func (a *DHTAnnouncer) Run(announceFunc func(), interval, minInterval time.Duration, l logger.Logger) {
	defer close(a.doneC)

	timer := time.NewTimer(minInterval)
	defer timer.Stop()

	resetTimer := func() {
		if a.needMorePeers {
			timer.Reset(time.Until(a.lastAnnounce.Add(minInterval)))
		} else {
			timer.Reset(time.Until(a.lastAnnounce.Add(interval)))
		}
	}

	announce := func() {
		announceFunc()
		a.lastAnnounce = time.Now()
		resetTimer()
	}

	announce()
	for {
		select {
		case <-timer.C:
			announce()
		case a.needMorePeers = <-a.needMorePeersC:
			resetTimer()
		case <-a.closeC:
			return
		}
	}
}
