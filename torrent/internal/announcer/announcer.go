package announcer

import (
	"net"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

const stopEventTimeout = time.Minute

type Announcer struct {
	url          string
	log          logger.Logger
	completedC   chan struct{}
	newPeers     chan []*net.TCPAddr
	tracker      tracker.Tracker
	backoff      backoff.BackOff
	nextAnnounce time.Duration
	requests     chan *Request
	closeC       chan struct{}
	closedC      chan struct{}
}

type Request struct {
	Response chan Response
}

type Response struct {
	Transfer tracker.Transfer
}

func New(trk tracker.Tracker, requests chan *Request, completedC chan struct{}, newPeers chan []*net.TCPAddr, l logger.Logger) *Announcer {
	return &Announcer{
		tracker:    trk,
		log:        l,
		completedC: completedC,
		newPeers:   newPeers,
		requests:   requests,
		closeC:     make(chan struct{}),
		closedC:    make(chan struct{}),
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

func (a *Announcer) Close() {
	close(a.closeC)
	<-a.closedC
}

func (a *Announcer) Run() {
	defer close(a.closedC)
	a.backoff.Reset()
	a.announce(tracker.EventStarted, a.closeC)
	for {
		select {
		case <-time.After(a.nextAnnounce):
			a.announce(tracker.EventNone, a.closeC)
		case <-a.completedC:
			a.announce(tracker.EventCompleted, a.closeC)
			a.completedC = nil
		case <-a.closeC:
			go a.announceStopAndClose()
			return
		}
	}
}

func (a *Announcer) announce(e tracker.Event, stopC chan struct{}) {
	req := &Request{
		Response: make(chan Response),
	}
	select {
	case a.requests <- req:
	case <-stopC:
		return
	}
	var resp Response
	select {
	case resp = <-req.Response:
	case <-stopC:
		return
	}
	r, err := a.tracker.Announce(resp.Transfer, e, stopC)
	if err == tracker.ErrRequestCancelled {
		return
	}
	if err != nil {
		a.log.Errorln("announce error:", err)
		a.nextAnnounce = a.backoff.NextBackOff()
	} else {
		a.backoff.Reset()
		a.nextAnnounce = r.Interval
		select {
		case a.newPeers <- r.Peers:
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
