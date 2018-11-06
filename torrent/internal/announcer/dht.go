package announcer

import (
	"time"

	"github.com/cenkalti/rain/internal/logger"
)

type DHTAnnouncer struct {
	lastAnnounce time.Time
	triggerC     chan struct{}
	closeC       chan struct{}
	doneC        chan struct{}
}

func NewDHTAnnouncer() *DHTAnnouncer {
	return &DHTAnnouncer{
		triggerC: make(chan struct{}),
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

func (a *DHTAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *DHTAnnouncer) Trigger() {
	select {
	case a.triggerC <- struct{}{}:
	default:
	}
}

func (a *DHTAnnouncer) Run(announceFunc func(), interval, minInterval time.Duration, l logger.Logger) {
	defer close(a.doneC)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.announce(announceFunc)
	for {
		select {
		case <-ticker.C:
			a.announce(announceFunc)
		case <-a.triggerC:
			if time.Since(a.lastAnnounce) > minInterval {
				a.announce(announceFunc)
			}
		case <-a.closeC:
			return
		}
	}
}

func (a *DHTAnnouncer) announce(announceFunc func()) {
	announceFunc()
	a.lastAnnounce = time.Now()
}
