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

	timer := time.NewTimer(interval)
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
