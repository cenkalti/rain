package announcer

import (
	"time"

	"github.com/cenkalti/rain/internal/logger"
)

type DHTAnnouncer struct {
	lastAnnounce   time.Time
	needMorePeers  bool
	needMorePeersC chan bool
	closeC         chan struct{}
	doneC          chan struct{}
}

func NewDHTAnnouncer() *DHTAnnouncer {
	return &DHTAnnouncer{
		needMorePeersC: make(chan bool),
		closeC:         make(chan struct{}),
		doneC:          make(chan struct{}),
	}
}

func (a *DHTAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *DHTAnnouncer) NeedMorePeers(val bool) {
	select {
	case a.needMorePeersC <- val:
	case <-a.doneC:
	}
}

func (a *DHTAnnouncer) Run(announceFunc func(), interval, minInterval time.Duration, l logger.Logger) {
	defer close(a.doneC)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	announce := func() {
		announceFunc()
		a.lastAnnounce = time.Now()
	}

	announce()
	for {
		select {
		case <-ticker.C:
			announce()
		case val := <-a.needMorePeersC:
			if val {
				if !a.needMorePeers {
					announce()
					ticker.Stop()
					ticker = time.NewTicker(minInterval)
				}
			} else {
				if a.needMorePeers {
					ticker.Stop()
					ticker = time.NewTicker(interval)
				}
			}
			a.needMorePeers = val
		case <-a.closeC:
			return
		}
	}
}
