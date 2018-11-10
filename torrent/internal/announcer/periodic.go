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
	NotContactedYet string = "Not contacted yet"
	Contacting             = "Contacting"
	Working                = "Working"
	NotWorking             = "Not working"
)

type PeriodicalAnnouncer struct {
	Tracker        tracker.Tracker
	status         string
	statsCommandC  chan statsRequest
	numWant        int
	interval       time.Duration
	minInterval    time.Duration
	seeders        int
	leechers       int
	lastError      error
	log            logger.Logger
	completedC     chan struct{}
	newPeers       chan []*net.TCPAddr
	backoff        backoff.BackOff
	requests       chan *Request
	lastAnnounce   time.Time
	HasAnnounced   bool
	needMorePeersC chan bool
	closeC         chan struct{}
	doneC          chan struct{}
}

type Request struct {
	Response chan Response
	Cancel   chan struct{}
}

type Response struct {
	Torrent tracker.Torrent
}

func NewPeriodicalAnnouncer(trk tracker.Tracker, numWant int, minInterval time.Duration, requests chan *Request, completedC chan struct{}, newPeers chan []*net.TCPAddr, l logger.Logger) *PeriodicalAnnouncer {
	return &PeriodicalAnnouncer{
		Tracker:        trk,
		status:         NotContactedYet,
		statsCommandC:  make(chan statsRequest),
		numWant:        numWant,
		minInterval:    minInterval,
		log:            l,
		completedC:     completedC,
		newPeers:       newPeers,
		requests:       requests,
		needMorePeersC: make(chan bool),
		closeC:         make(chan struct{}),
		doneC:          make(chan struct{}),
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

type statsRequest struct {
	Response chan Stats
}

func (a *PeriodicalAnnouncer) Stats() Stats {
	var stats Stats
	req := statsRequest{Response: make(chan Stats, 1)}
	select {
	case a.statsCommandC <- req:
	case <-a.closeC:
	}
	select {
	case stats = <-req.Response:
	case <-a.closeC:
	}
	return stats
}

func (a *PeriodicalAnnouncer) NeedMorePeers(val bool) {
	select {
	case a.needMorePeersC <- val:
	case <-a.doneC:
	}
}

func (a *PeriodicalAnnouncer) Run() {
	defer close(a.doneC)
	a.backoff.Reset()

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	var timerC <-chan time.Time
	setTimer := func(d time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(d)
		timerC = timer.C
	}

	var needMorePeers bool

	announcer := newAnnouncer(a.Tracker, a.requests, a.newPeers)
	defer announcer.Cancel()

	announcer.Announce(tracker.EventStarted, a.numWant)
	for {
		select {
		case <-timerC:
			a.status = Contacting
			announcer.Announce(tracker.EventNone, a.numWant)
		case resp := <-announcer.ResponseC:
			announcer.announcing = false
			a.lastAnnounce = time.Now()
			a.seeders = int(resp.Seeders)
			a.leechers = int(resp.Leechers)
			a.interval = resp.Interval
			if resp.MinInterval > 0 {
				a.minInterval = resp.MinInterval
			}
			a.HasAnnounced = true
			a.lastError = nil
			a.status = Working
			a.backoff.Reset()
			if needMorePeers {
				setTimer(a.minInterval)
			} else {
				setTimer(a.interval)
			}
		case a.lastError = <-announcer.ErrorC:
			announcer.announcing = false
			a.status = NotWorking
			if _, ok := a.lastError.(*net.OpError); ok {
				a.log.Debugln("net operation error:", a.lastError)
			} else {
				a.log.Errorln("announce error:", a.lastError)
			}
			setTimer(a.backoff.NextBackOff())
		case needMorePeers = <-a.needMorePeersC:
			if announcer.announcing {
				break
			}
			if needMorePeers {
				setTimer(time.Until(a.lastAnnounce.Add(a.minInterval)))
			} else {
				setTimer(time.Until(a.lastAnnounce.Add(a.interval)))
			}
		case <-a.completedC:
			announcer.Cancel()
			a.status = Contacting
			announcer.Announce(tracker.EventCompleted, 0)
			a.completedC = nil
		case req := <-a.statsCommandC:
			req.Response <- a.stats()
		case <-a.closeC:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

type Stats struct {
	Status   string
	Error    *string
	Seeders  int
	Leechers int
}

func (a *PeriodicalAnnouncer) stats() Stats {
	st := Stats{
		Status:   a.status,
		Seeders:  a.seeders,
		Leechers: a.leechers,
	}
	if a.lastError != nil {
		s := a.lastError.Error()
		st.Error = &s
	}
	return st
}

type announcer struct {
	ResponseC  chan *tracker.AnnounceResponse
	ErrorC     chan error
	requestC   chan *Request
	newPeers   chan []*net.TCPAddr
	tracker    tracker.Tracker
	announcing bool
	stopC      chan struct{}
	doneC      chan struct{}
}

func newAnnouncer(trk tracker.Tracker, requestC chan *Request, newPeers chan []*net.TCPAddr) *announcer {
	return &announcer{
		tracker:   trk,
		requestC:  requestC,
		newPeers:  newPeers,
		ResponseC: make(chan *tracker.AnnounceResponse),
		ErrorC:    make(chan error),
	}
}

func (a *announcer) Announce(e tracker.Event, numWant int) {
	a.Cancel()
	a.announcing = true
	a.stopC = make(chan struct{})
	a.doneC = make(chan struct{})
	go announce(a.tracker, e, numWant, a.requestC, a.newPeers, a.ResponseC, a.ErrorC, a.stopC, a.doneC)
}

func (a *announcer) Cancel() {
	if a.announcing {
		close(a.stopC)
		<-a.doneC
		a.announcing = false
	}
}

func announce(trk tracker.Tracker, e tracker.Event, numWant int, requestC chan *Request, newPeers chan []*net.TCPAddr, responseC chan *tracker.AnnounceResponse, errC chan error, stopC, doneC chan struct{}) {
	defer close(doneC)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-doneC:
		case <-stopC:
			cancel()
		}
	}()

	req := &Request{
		Response: make(chan Response),
		Cancel:   make(chan struct{}),
	}
	defer close(req.Cancel)

	select {
	case requestC <- req:
	case <-stopC:
		return
	}

	var resp Response
	select {
	case resp = <-req.Response:
	case <-stopC:
		return
	}

	annReq := tracker.AnnounceRequest{
		Torrent: resp.Torrent,
		Event:   e,
		NumWant: numWant,
	}
	annResp, err := trk.Announce(ctx, annReq)
	if err == context.Canceled {
		return
	}
	if err != nil {
		select {
		case errC <- err:
		case <-stopC:
		}
		return
	}
	select {
	case newPeers <- annResp.Peers:
	case <-stopC:
	}
	select {
	case responseC <- annResp:
	case <-stopC:
	}
}
