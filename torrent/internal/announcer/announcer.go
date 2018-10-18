package announcer

import (
	"context"
	"net"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

const (
	stopEventTimeout    = 5 * time.Second
	minAnnounceInterval = time.Minute
)

type Announcer struct {
	log          logger.Logger
	completedC   chan struct{}
	newPeers     chan []*net.TCPAddr
	tracker      tracker.Tracker
	backoff      backoff.BackOff
	nextAnnounce time.Duration
	requests     chan *Request
	lastAnnounce time.Time
	triggerC     chan struct{}
	closeC       chan struct{}
	doneC        chan struct{}
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
		triggerC:   make(chan struct{}),
		closeC:     make(chan struct{}),
		doneC:      make(chan struct{}),
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
	<-a.doneC
}

func (a *Announcer) Trigger() {
	select {
	case a.triggerC <- struct{}{}:
	default:
	}
}

func (a *Announcer) Run() {
	defer close(a.doneC)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-a.closeC
		cancel()
	}()

	a.backoff.Reset()
	a.announce(ctx, tracker.EventStarted)
	for {
		select {
		case <-time.After(a.nextAnnounce):
			a.announce(ctx, tracker.EventNone)
		case <-a.triggerC:
			if time.Since(a.lastAnnounce) > minAnnounceInterval {
				a.announce(ctx, tracker.EventNone)
			}
		case <-a.completedC:
			a.announce(ctx, tracker.EventCompleted)
			a.completedC = nil
		case <-a.closeC:
			return
		}
	}
}

func (a *Announcer) announce(ctx context.Context, e tracker.Event) {
	req := &Request{
		Response: make(chan Response),
	}
	select {
	case a.requests <- req:
	case <-ctx.Done():
		return
	}
	var resp Response
	select {
	case resp = <-req.Response:
	case <-ctx.Done():
		return
	}
	a.lastAnnounce = time.Now()
	r, err := a.tracker.Announce(ctx, resp.Transfer, e)
	if err == context.Canceled {
		return
	}
	if err != nil {
		if _, ok := err.(*net.OpError); ok {
			a.log.Debugln("net operation error:", err)
		} else {
			a.log.Errorln("announce error:", err)
		}
		a.nextAnnounce = a.backoff.NextBackOff()
		return
	}
	a.backoff.Reset()
	a.nextAnnounce = r.Interval
	select {
	case a.newPeers <- r.Peers:
	case <-a.closeC:
	case <-ctx.Done():
	}
}

type StopAnnouncer struct {
	log      logger.Logger
	trackers []tracker.Tracker
	transfer tracker.Transfer
	resultC  chan struct{}
	closeC   chan struct{}
	doneC    chan struct{}
}

func NewStopAnnouncer(trackers []tracker.Tracker, tra tracker.Transfer, resultC chan struct{}, l logger.Logger) *StopAnnouncer {
	return &StopAnnouncer{
		log:      l,
		trackers: trackers,
		transfer: tra,
		resultC:  resultC,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

func (a *StopAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *StopAnnouncer) Run() {
	defer close(a.doneC)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(stopEventTimeout))
	go func() {
		<-a.closeC
		cancel()
	}()

	doneC := make(chan struct{})
	for _, trk := range a.trackers {
		go func(trk tracker.Tracker) {
			defer func() { doneC <- struct{}{} }()
			_, err := trk.Announce(ctx, a.transfer, tracker.EventStopped)
			if err == context.Canceled {
				return
			}
			if err != nil {
				if _, ok := err.(*net.OpError); ok {
					a.log.Debugln("net operation error:", err)
				} else {
					a.log.Errorln("announce error:", err)
				}
			}
		}(trk)
	}
	for range a.trackers {
		<-doneC
	}
	select {
	case a.resultC <- struct{}{}:
	case <-a.closeC:
	}
}
