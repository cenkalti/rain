package announcer

import (
	"math"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
)

type Status int

const (
	NotContactedYet Status = iota
	Contacting
	Working
	NotWorking
)

type PeriodicalAnnouncer struct {
	Tracker       tracker.Tracker
	status        Status
	statsCommandC chan statsRequest
	numWant       int
	interval      time.Duration
	minInterval   time.Duration
	seeders       int
	leechers      int
	lastError     error
	log           logger.Logger
	completedC    chan struct{}
	newPeers      chan []*net.TCPAddr
	backoff       backoff.BackOff
	requests      chan *Request
	lastAnnounce  time.Time
	HasAnnounced  bool
	closeC        chan struct{}
	doneC         chan struct{}

	needMorePeers  bool
	mNeedMorePeers sync.RWMutex
	needMorePeersC chan struct{}
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
		needMorePeersC: make(chan struct{}, 1),
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
	a.mNeedMorePeers.Lock()
	a.needMorePeers = val
	a.mNeedMorePeers.Unlock()
	select {
	case a.needMorePeersC <- struct{}{}:
	case <-a.doneC:
	default:
	}
}

func (a *PeriodicalAnnouncer) Run() {
	defer close(a.doneC)
	a.backoff.Reset()

	timer := time.NewTimer(math.MaxInt64)
	defer timer.Stop()

	ca := newCancelableAnnouncer(a.Tracker, a.requests, a.newPeers)
	defer ca.Cancel()

	ca.Announce(tracker.EventStarted, a.numWant)
	for {
		select {
		case <-timer.C:
			a.status = Contacting
			ca.Announce(tracker.EventNone, a.numWant)
		case resp := <-ca.ResponseC:
			ca.announcing = false
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
			a.mNeedMorePeers.RLock()
			needMorePeers := a.needMorePeers
			a.mNeedMorePeers.RUnlock()
			if needMorePeers {
				timer.Reset(a.minInterval)
			} else {
				timer.Reset(a.interval)
			}
		case a.lastError = <-ca.ErrorC:
			ca.announcing = false
			a.lastAnnounce = time.Now()
			a.status = NotWorking
			a.log.Debugln("announce error:", a.lastError)
			timer.Reset(a.backoff.NextBackOff())
		case <-a.needMorePeersC:
			if ca.announcing {
				break
			}
			a.mNeedMorePeers.RLock()
			needMorePeers := a.needMorePeers
			a.mNeedMorePeers.RUnlock()
			if needMorePeers {
				timer.Reset(time.Until(a.lastAnnounce.Add(a.minInterval)))
			} else {
				timer.Reset(time.Until(a.lastAnnounce.Add(a.interval)))
			}
		case <-a.completedC:
			ca.Cancel()
			a.status = Contacting
			ca.Announce(tracker.EventCompleted, 0)
			a.completedC = nil
		case req := <-a.statsCommandC:
			req.Response <- a.stats()
		case <-a.closeC:
			return
		}
	}
}

type Stats struct {
	Status   Status
	Error    error
	Seeders  int
	Leechers int
}

func (a *PeriodicalAnnouncer) stats() Stats {
	return Stats{
		Status:   a.status,
		Error:    a.lastError,
		Seeders:  a.seeders,
		Leechers: a.leechers,
	}
}
