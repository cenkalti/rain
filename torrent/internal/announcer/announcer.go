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
	Tracker      tracker.Tracker
	log          logger.Logger
	completedC   chan struct{}
	newPeers     chan []*net.TCPAddr
	backoff      backoff.BackOff
	nextAnnounce time.Duration
	requests     chan *Request
	lastAnnounce time.Time
	HasAnnounced bool
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
		Tracker:    trk,
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
	r, err := callAnnounce(ctx, a.Tracker, resp.Transfer, e, a.log)
	if err != nil {
		a.nextAnnounce = a.backoff.NextBackOff()
		return
	}
	a.HasAnnounced = true
	a.backoff.Reset()
	a.nextAnnounce = r.Interval
	select {
	case a.newPeers <- r.Peers:
	case <-a.closeC:
	case <-ctx.Done():
	}
}

func callAnnounce(ctx context.Context, trk tracker.Tracker, t tracker.Transfer, e tracker.Event, l logger.Logger) (*tracker.AnnounceResponse, error) {
	r, err := trk.Announce(ctx, t, e)
	if err == context.Canceled {
		return nil, err
	}
	if err != nil {
		if _, ok := err.(*net.OpError); ok {
			l.Debugln("net operation error:", err)
		} else {
			l.Errorln("announce error:", err)
		}
	}
	return r, err
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
			callAnnounce(ctx, trk, a.transfer, tracker.EventStopped, a.log)
			doneC <- struct{}{}
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
