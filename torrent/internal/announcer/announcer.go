package announcer

import (
	"context"
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
		case <-a.completedC:
			a.announce(ctx, tracker.EventCompleted)
			a.completedC = nil
		case <-a.closeC:
			go a.announceStopAndClose()
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
	} else {
		a.backoff.Reset()
		a.nextAnnounce = r.Interval
		select {
		case a.newPeers <- r.Peers:
		case <-ctx.Done():
		}
	}
}

func (a *Announcer) announceStopAndClose() {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(stopEventTimeout))
	defer cancel()
	a.announce(ctx, tracker.EventStopped)
	a.tracker.Close()
}
