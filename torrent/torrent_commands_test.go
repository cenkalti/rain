package torrent

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addStoppedTorrent returns a torrent that has never been started. Its run loop
// is live either way — newTorrent starts it at construction — so every command
// is serviceable without touching the network or the disk.
func addStoppedTorrent(t *testing.T, s *Session) *Torrent {
	t.Helper()
	f, err := os.Open(torrentFile)
	require.NoError(t, err)
	defer f.Close()
	tor, err := s.AddTorrent(f, &AddTorrentOptions{Stopped: true})
	require.NoError(t, err)
	return tor
}

// mustReturn fails the test if fn has not returned within the package timeout.
// Every command must come back whether the run loop is serving or already gone.
func mustReturn(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("%s did not return within %s", name, timeout)
	}
}

// commands names every method that reaches the run loop through a command
// channel, so the tests below can walk the whole set rather than a sample.
// Start and Verify are excluded: they spawn allocator and verifier work against
// real files, which the concurrency tests here are not trying to exercise.
func commands(tor *torrent) map[string]func() {
	return map[string]func(){
		"Stats":        func() { tor.Stats() },
		"Trackers":     func() { tor.Trackers() },
		"Peers":        func() { tor.Peers() },
		"Webseeds":     func() { tor.Webseeds() },
		"Announce":     func() { tor.Announce() },
		"Stop":         func() { tor.Stop() },
		"NotifyError":  func() { tor.NotifyError() },
		"NotifyListen": func() { tor.NotifyListen() },
		"AddPeers": func() {
			tor.AddPeers([]*net.TCPAddr{{IP: net.IPv4(127, 0, 0, 1), Port: 1234}})
		},
		"AddTrackers": func() { tor.AddTrackers(nil) },
	}
}

func TestCommandsOnStoppedTorrent(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)

	for name, fn := range commands(tor.torrent) {
		mustReturn(t, name, fn)
	}

	// The queries must report the torrent we actually added.
	stats := tor.Stats()
	assert.Equal(t, torrentName, stats.Name)
	assert.Equal(t, torrentInfoHashString, tor.InfoHash().String())
	assert.Empty(t, tor.Peers(), "a stopped torrent has no connected peers")
}

func TestCommandsReturnAfterClose(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	id := tor.ID()
	inner := tor.torrent

	require.NoError(t, s.RemoveTorrent(id, true))

	// With the run loop gone, every command must still return promptly rather
	// than block forever on a channel nobody is receiving from.
	for name, fn := range commands(inner) {
		mustReturn(t, name+" after close", fn)
	}
}

func TestQueriesReturnZeroValuesAfterClose(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	inner := tor.torrent

	require.NoError(t, s.RemoveTorrent(tor.ID(), true))

	assert.Equal(t, Stats{}, inner.Stats())
	assert.Nil(t, inner.Trackers())
	assert.Nil(t, inner.Peers())
	assert.Nil(t, inner.Webseeds())

	// The notify commands report their own failure with a nil channel, which
	// callers select on; a non-nil channel here would block them forever.
	assert.Nil(t, inner.NotifyError())
	assert.Nil(t, inner.NotifyListen())
}

func TestCommandsConcurrent(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	cmds := commands(tor.torrent)

	const goroutines = 8
	const iterations = 25

	var wg sync.WaitGroup
	for _, fn := range cmds {
		for range goroutines {
			wg.Add(1)
			go func(fn func()) {
				defer wg.Done()
				for range iterations {
					fn()
				}
			}(fn)
		}
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("%d concurrent commands did not drain within %s", len(cmds)*goroutines*iterations, timeout)
	}

	// The loop is still serving after the hammering.
	assert.Equal(t, torrentName, tor.Stats().Name)
}

func TestCommandsRacingWithClose(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	inner := tor.torrent
	cmds := commands(inner)

	// Senders keep issuing commands while the torrent is torn down underneath
	// them. None may block past the close, and none may panic.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for _, fn := range cmds {
		wg.Add(1)
		go func(fn func()) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					fn()
				}
			}
		}(fn)
	}

	time.Sleep(10 * time.Millisecond) // let the senders get going
	require.NoError(t, s.RemoveTorrent(tor.ID(), true))
	close(stop)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("commands racing with close did not all return within %s", timeout)
	}
}

func TestNotifyListenDeliversPort(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	tor.torrent.trackers = nil

	require.NoError(t, tor.Start())

	select {
	case port := <-tor.torrent.NotifyListen():
		assert.NotZero(t, port, "listen port must be reported")
	case err := <-tor.torrent.NotifyError():
		t.Fatal(err)
	case <-time.After(timeout):
		t.Fatal("NotifyListen did not deliver a port")
	}
}

// errC and portC are created by start() and dropped by stop(), so the notify
// commands hand back whatever the torrent holds at the moment the run loop
// services them — nil before Start, live after. That is why these handlers have
// to run on the loop goroutine rather than read the fields from the caller.
func TestNotifyChannelsFollowRunState(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	tor.torrent.trackers = nil
	inner := tor.torrent

	assert.Nil(t, inner.NotifyError(), "no error channel before Start")
	assert.Nil(t, inner.NotifyListen(), "no port channel before Start")

	require.NoError(t, tor.Start())
	errC := inner.NotifyError()
	require.NotNil(t, errC, "Start must publish an error channel")
	require.NotNil(t, inner.NotifyListen(), "Start must publish a port channel")

	// Repeated calls hand back the same channel, not a fresh one per call.
	assert.Equal(t, errC, inner.NotifyError())

	// Stop does not block: the torrent enters Stopping, announces the stop
	// event to trackers, and only then does handleStopped drop the channels.
	require.NoError(t, tor.Stop())
	require.Eventually(t, func() bool {
		return inner.NotifyError() == nil && inner.NotifyListen() == nil
	}, timeout, 10*time.Millisecond, "Stop must eventually retract both channels")
}

// The session health check proves the run loop is alive by sending it a command
// and not waiting for any reply. A command that the loop cannot accept promptly
// is what makes checkTorrent crash the process, so the probe must stay cheap
// and must never block the loop on a response nobody reads.
func TestRunLoopAcceptsLivenessProbe(t *testing.T) {
	s := newTestSession(t)
	tor := addStoppedTorrent(t, s)
	inner := tor.torrent

	for range 10 {
		select {
		case inner.notifyErrorCommandC <- notifyErrorCommand{errCC: make(chan chan error, 1)}:
		case <-inner.closeC:
			t.Fatal("torrent closed unexpectedly")
		case <-time.After(timeout):
			t.Fatal("run loop did not accept the liveness probe")
		}
	}
}
