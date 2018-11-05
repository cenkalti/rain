package announcer

import (
	"context"
	"net"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

type PeriodicalAnnouncer struct {
	Tracker      tracker.Tracker
	numWant      int
	minInterval  time.Duration
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
	Torrent tracker.Torrent
}

func NewPeriodicalAnnouncer(trk tracker.Tracker, numWant int, minInterval time.Duration, requests chan *Request, completedC chan struct{}, newPeers chan []*net.TCPAddr, l logger.Logger) *PeriodicalAnnouncer {
	return &PeriodicalAnnouncer{
		Tracker:     trk,
		numWant:     numWant,
		minInterval: minInterval,
		log:         l,
		completedC:  completedC,
		newPeers:    newPeers,
		requests:    requests,
		triggerC:    make(chan struct{}),
		closeC:      make(chan struct{}),
		doneC:       make(chan struct{}),
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

func (a *PeriodicalAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *PeriodicalAnnouncer) Trigger() {
	select {
	case a.triggerC <- struct{}{}:
	default:
	}
}

func (a *PeriodicalAnnouncer) Run() {
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
			if time.Since(a.lastAnnounce) > a.minInterval {
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

func (a *PeriodicalAnnouncer) announce(ctx context.Context, e tracker.Event) {
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
	r, err := callAnnounce(ctx, a.Tracker, resp.Torrent, e, a.numWant, a.log)
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

func callAnnounce(ctx context.Context, trk tracker.Tracker, t tracker.Torrent, e tracker.Event, numWant int, l logger.Logger) (*tracker.AnnounceResponse, error) {
	req := tracker.AnnounceRequest{
		Torrent: t,
		Event:   e,
		NumWant: numWant,
	}
	resp, err := trk.Announce(ctx, req)
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
	return resp, err
}
