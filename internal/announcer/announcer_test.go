package announcer

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/resolver"
	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 5 * time.Second

// fakeTracker is a tracker.Tracker stub that returns a preconfigured response or
// error and records the announce events it receives.
type fakeTracker struct {
	url    string
	resp   *tracker.AnnounceResponse
	err    error
	events chan tracker.Event
}

func (f *fakeTracker) Announce(_ context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	if f.events != nil {
		select {
		case f.events <- req.Event:
		default:
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeTracker) URL() string { return f.url }

func testTorrent() tracker.Torrent { return tracker.Torrent{} }

func TestPeriodicalAnnouncerSuccess(t *testing.T) {
	resp := &tracker.AnnounceResponse{
		Interval: time.Hour, // large, so no re-announce happens during the test
		Seeders:  3,
		Leechers: 2,
		Peers:    []*net.TCPAddr{{IP: net.IPv4(1, 2, 3, 4), Port: 6881}},
	}
	trk := &fakeTracker{url: "http://t/announce", resp: resp, events: make(chan tracker.Event, 8)}

	newPeers := make(chan []*net.TCPAddr, 4)
	a := NewPeriodicalAnnouncer(trk, 50, time.Minute, testTorrent, make(chan struct{}, 1), newPeers, logger.New("test"))
	go a.Run()
	t.Cleanup(a.Close)

	// The first announce is a "started" event.
	assert.Equal(t, tracker.EventStarted, recvEvent(t, trk))

	// Peers from the response are forwarded.
	select {
	case peers := <-newPeers:
		assert.Len(t, peers, 1)
	case <-time.After(testTimeout):
		t.Fatal("peers were not forwarded")
	}

	// Stats reflect the successful announce (read via the command channel).
	stats := a.Stats()
	assert.Equal(t, Working, stats.Status)
	assert.Equal(t, 3, stats.Seeders)
	assert.Equal(t, 2, stats.Leechers)
}

func TestPeriodicalAnnouncerSendsCompleted(t *testing.T) {
	resp := &tracker.AnnounceResponse{Interval: time.Hour}
	trk := &fakeTracker{resp: resp, events: make(chan tracker.Event, 8)}

	completedC := make(chan struct{}, 1)
	newPeers := make(chan []*net.TCPAddr, 4)
	a := NewPeriodicalAnnouncer(trk, 50, time.Minute, testTorrent, completedC, newPeers, logger.New("test"))
	go a.Run()
	t.Cleanup(a.Close)

	assert.Equal(t, tracker.EventStarted, recvEvent(t, trk))

	// Signal completion; the announcer should send a "completed" event.
	completedC <- struct{}{}
	assert.Equal(t, tracker.EventCompleted, recvEvent(t, trk))
}

func TestPeriodicalAnnouncerError(t *testing.T) {
	trk := &fakeTracker{url: "http://t/announce", err: errors.New("connection refused")}

	a := NewPeriodicalAnnouncer(trk, 50, time.Minute, testTorrent, make(chan struct{}, 1), make(chan []*net.TCPAddr, 4), logger.New("test"))
	go a.Run()
	t.Cleanup(a.Close)

	// The announce fails, so the status becomes NotWorking with a recorded error.
	require.Eventually(t, func() bool {
		return a.Stats().Status == NotWorking
	}, 2*time.Second, 10*time.Millisecond)
	assert.NotNil(t, a.Stats().Error)
}

func TestNewAnnounceError(t *testing.T) {
	a := &PeriodicalAnnouncer{Tracker: &fakeTracker{url: "http://tracker.example/announce"}}
	cases := []struct {
		name    string
		err     error
		message string
		unknown bool
	}{
		{"decode", tracker.ErrDecode, "invalid response from tracker", false},
		{"blocked", resolver.ErrBlocked, "tracker IP is blocked", false},
		{"tracker error", &tracker.Error{FailureReason: "not registered"}, "announce error: not registered", false},
		{"unknown", errors.New("something weird"), "unknown error in announce", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := a.newAnnounceError(tc.err)
			assert.Equal(t, tc.message, e.Message)
			assert.Equal(t, tc.unknown, e.Unknown)
		})
	}
}

func TestAnnounceErrorWithType(t *testing.T) {
	e := &AnnounceError{Err: errors.New("boom")}
	assert.Equal(t, "*errors.errorString: boom", e.ErrorWithType())
}

func TestStopAnnouncer(t *testing.T) {
	t0 := &fakeTracker{url: "t0", events: make(chan tracker.Event, 1)}
	t1 := &fakeTracker{url: "t1", events: make(chan tracker.Event, 1)}

	resultC := make(chan struct{}, 1)
	a := NewStopAnnouncer([]tracker.Tracker{t0, t1}, testTorrent(), testTimeout, resultC, logger.New("test"))
	go a.Run()
	t.Cleanup(a.Close)

	select {
	case <-resultC:
	case <-time.After(testTimeout):
		t.Fatal("stop announcer did not finish")
	}
	assert.Equal(t, tracker.EventStopped, recvEvent(t, t0))
	assert.Equal(t, tracker.EventStopped, recvEvent(t, t1))
}

func TestDHTAnnouncer(t *testing.T) {
	var mu sync.Mutex
	var count int
	announceFunc := func() {
		mu.Lock()
		count++
		mu.Unlock()
	}

	a := NewDHTAnnouncer()
	// Large intervals so only the initial announce runs during the test.
	go a.Run(announceFunc, time.Hour, time.Hour, logger.New("test"))
	t.Cleanup(a.Close)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return count >= 1
	}, testTimeout, 10*time.Millisecond)

	// Toggling the need-more-peers flag must not block.
	a.NeedMorePeers(false)
}

func recvEvent(t *testing.T, f *fakeTracker) tracker.Event {
	t.Helper()
	select {
	case e := <-f.events:
		return e
	case <-time.After(testTimeout):
		t.Fatal("tracker was not contacted")
		return 0
	}
}
