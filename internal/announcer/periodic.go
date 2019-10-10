package announcer

import (
	"context"
	"math"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v3"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/resolver"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
)

// Status of the announcer.
type Status int

const (
	// NotContactedYet with the tracker.
	NotContactedYet Status = iota
	// Contacting the tracker.
	Contacting
	// Working as expected.
	Working
	// NotWorking as expected.
	NotWorking
)

// PeriodicalAnnouncer announces the Torrent to the Tracker periodically.
type PeriodicalAnnouncer struct {
	Tracker       tracker.Tracker
	status        Status
	statsCommandC chan statsRequest
	numWant       int
	interval      time.Duration
	minInterval   time.Duration
	seeders       int
	leechers      int
	warningMsg    string
	lastError     *AnnounceError
	log           logger.Logger
	completedC    chan struct{}
	newPeers      chan []*net.TCPAddr
	backoff       backoff.BackOff
	getTorrent    func() tracker.Torrent
	lastAnnounce  time.Time
	nextAnnounce  time.Time
	HasAnnounced  bool
	responseC     chan *tracker.AnnounceResponse
	errC          chan error
	closeC        chan struct{}
	doneC         chan struct{}

	needMorePeers  bool
	mNeedMorePeers sync.RWMutex
	needMorePeersC chan struct{}
}

// NewPeriodicalAnnouncer returns a new PeriodicalAnnouncer.
func NewPeriodicalAnnouncer(trk tracker.Tracker, numWant int, minInterval time.Duration, getTorrent func() tracker.Torrent, completedC chan struct{}, newPeers chan []*net.TCPAddr, l logger.Logger) *PeriodicalAnnouncer {
	return &PeriodicalAnnouncer{
		Tracker:        trk,
		status:         NotContactedYet,
		statsCommandC:  make(chan statsRequest),
		numWant:        numWant,
		minInterval:    minInterval,
		log:            l,
		completedC:     completedC,
		newPeers:       newPeers,
		getTorrent:     getTorrent,
		needMorePeersC: make(chan struct{}, 1),
		responseC:      make(chan *tracker.AnnounceResponse),
		errC:           make(chan error),
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

// Close the announcer.
func (a *PeriodicalAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

type statsRequest struct {
	Response chan Stats
}

// Stats about the tracker and the announce operation.
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

// NeedMorePeers signals the announcer goroutine about the need of more peers.
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

// Run the announcer goroutine. Invoke with go statement.
func (a *PeriodicalAnnouncer) Run() {
	defer close(a.doneC)
	a.backoff.Reset()

	timer := time.NewTimer(math.MaxInt64)
	defer timer.Stop()

	resetTimer := func(interval time.Duration) {
		timer.Reset(interval)
		if interval < 0 {
			a.nextAnnounce = time.Now()
		} else {
			a.nextAnnounce = time.Now().Add(interval)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	// BEP 0003: No completed is sent if the file was complete when started.
	select {
	case <-a.completedC:
		a.completedC = nil
	default:
	}

	a.doAnnounce(ctx, tracker.EventStarted, a.numWant)
	for {
		select {
		case <-timer.C:
			if a.status == Contacting {
				break
			}
			a.doAnnounce(ctx, tracker.EventNone, a.numWant)
		case resp := <-a.responseC:
			a.status = Working
			a.seeders = int(resp.Seeders)
			a.leechers = int(resp.Leechers)
			a.warningMsg = resp.WarningMessage
			if a.warningMsg != "" {
				a.log.Debugln("announce warning:", a.warningMsg)
			}
			a.interval = resp.Interval
			if resp.MinInterval > 0 {
				a.minInterval = resp.MinInterval
			}
			a.HasAnnounced = true
			a.lastError = nil
			a.backoff.Reset()
			interval := a.getNextInterval()
			resetTimer(interval)
		case err := <-a.errC:
			a.status = NotWorking
			// Give more friendly error to the user
			a.lastError = a.newAnnounceError(err)
			if a.lastError.Unknown {
				a.log.Errorln("announce error:", a.lastError.ErrorWithType())
			} else {
				a.log.Debugln("announce error:", a.lastError.Err.Error())
			}
			interval := a.getNextIntervalFromError(a.lastError)
			resetTimer(interval)
		case <-a.needMorePeersC:
			if a.status == Contacting || a.status == NotWorking {
				break
			}
			interval := time.Until(a.lastAnnounce.Add(a.getNextInterval()))
			resetTimer(interval)
		case <-a.completedC:
			if a.status == Contacting {
				cancel()
				ctx, cancel = context.WithCancel(context.Background())
			}
			a.doAnnounce(ctx, tracker.EventCompleted, 0)
			a.completedC = nil // do not send more than one "completed" event
		case req := <-a.statsCommandC:
			req.Response <- a.stats()
		case <-a.closeC:
			cancel()
			return
		}
	}
}

func (a *PeriodicalAnnouncer) getNextInterval() time.Duration {
	a.mNeedMorePeers.RLock()
	need := a.needMorePeers
	a.mNeedMorePeers.RUnlock()
	if need {
		return a.minInterval
	}
	return a.interval
}

func (a *PeriodicalAnnouncer) getNextIntervalFromError(err *AnnounceError) time.Duration {
	if terr, ok := err.Err.(*tracker.Error); ok && terr.RetryIn > 0 {
		return terr.RetryIn
	}
	return a.backoff.NextBackOff()
}

func (a *PeriodicalAnnouncer) doAnnounce(ctx context.Context, event tracker.Event, numWant int) {
	go a.announce(ctx, event, numWant)
	a.status = Contacting
	a.lastAnnounce = time.Now()
}

func (a *PeriodicalAnnouncer) announce(ctx context.Context, event tracker.Event, numWant int) {
	announce(ctx, a.Tracker, event, numWant, a.getTorrent(), a.responseC, a.errC)
}

// Stats about the announcer.
type Stats struct {
	Status       Status
	Error        *AnnounceError
	Warning      string
	Seeders      int
	Leechers     int
	LastAnnounce time.Time
	NextAnnounce time.Time
}

func (a *PeriodicalAnnouncer) stats() Stats {
	return Stats{
		Status:       a.status,
		Error:        a.lastError,
		Warning:      a.warningMsg,
		Seeders:      a.seeders,
		Leechers:     a.leechers,
		LastAnnounce: a.lastAnnounce,
		NextAnnounce: a.nextAnnounce,
	}
}

// AnnounceError the error that comes from the Tracker itself.
type AnnounceError struct {
	Err     error
	Message string
	Unknown bool
}

func (a *PeriodicalAnnouncer) newAnnounceError(err error) (e *AnnounceError) {
	e = &AnnounceError{Err: err}
	switch err {
	case resolver.ErrNotIPv4Address:
		parsed, _ := url.Parse(a.Tracker.URL())
		e.Message = "tracker has no IPv4 address: " + parsed.Hostname()
		return
	case resolver.ErrBlocked:
		e.Message = "tracker IP is blocked"
		return
	case resolver.ErrInvalidPort:
		parsed, _ := url.Parse(a.Tracker.URL())
		e.Message = "invalid port number in tracker address: " + parsed.Host
		return
	case tracker.ErrDecode:
		e.Message = "invalid response from tracker"
		return
	}
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		e.Message = "timeout contacting tracker"
		return
	}
	switch err := err.(type) {
	case *net.DNSError:
		s := err.Error()
		if strings.HasSuffix(s, "no such host") {
			e.Message = "host not found: " + err.Name
			return
		}
		if strings.HasSuffix(s, "server misbehaving") {
			e.Message = "host not found: " + err.Name
			return
		}
	case *net.AddrError:
		s := err.Error()
		if strings.HasSuffix(s, "missing port in address") {
			e.Message = "missing port in tracker address"
			return
		}

	case *url.Error:
		s := err.Error()
		if strings.HasSuffix(s, "connection refused") {
			e.Message = "tracker refused the connection"
			return
		}
		if strings.HasSuffix(s, "no such host") {
			parsed, _ := url.Parse(a.Tracker.URL())
			e.Message = "no such host: " + parsed.Hostname()
			return
		}
		if strings.HasSuffix(s, "server misbehaving") {
			parsed, _ := url.Parse(a.Tracker.URL())
			e.Message = "server misbehaving: " + parsed.Hostname()
			return
		}
		if strings.HasSuffix(s, "tls: handshake failure") {
			e.Message = "TLS handshake has failed"
			return
		}
		if strings.HasSuffix(s, "no route to host") {
			parsed, _ := url.Parse(a.Tracker.URL())
			e.Message = "no route to host: " + parsed.Hostname()
			return
		}
		if strings.HasSuffix(s, resolver.ErrNotIPv4Address.Error()) {
			parsed, _ := url.Parse(a.Tracker.URL())
			e.Message = "tracker has no IPv4 address: " + parsed.Hostname()
			return
		}
		if strings.HasSuffix(s, "connection reset by peer") {
			e.Message = "tracker closed the connection"
			return
		}
		if strings.HasSuffix(s, "EOF") {
			e.Message = "tracker closed the connection"
			return
		}
		if strings.HasSuffix(s, "server gave HTTP response to HTTPS client") {
			e.Message = "invalid server response"
			return
		}
		if strings.Contains(s, "malformed HTTP status code") {
			e.Message = "invalid server response"
			return
		}
	case *httptracker.StatusError:
		if err.Code >= 400 {
			e.Message = "tracker returned HTTP status: " + strconv.Itoa(err.Code)
			return
		}
	case *tracker.Error:
		e.Message = "announce error: " + err.FailureReason
		return
	}
	e.Message = "unknown error in announce"
	e.Unknown = true
	return
}

// ErrorWithType returns the error string that is prefixed with type name.
func (e *AnnounceError) ErrorWithType() string {
	return reflect.TypeOf(e.Err).String() + ": " + e.Err.Error()
}
