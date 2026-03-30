package announcer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/logger"
	"github.com/cenkalti/rain/v2/internal/tracker"
)

type disabledTracker struct {
	url string
}

func (t disabledTracker) Announce(_ context.Context, _ tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	return nil, tracker.ErrUDPDisabled
}

func (t disabledTracker) URL() string {
	return t.url
}

func TestDisabledTrackerStops(t *testing.T) {
	trk := disabledTracker{url: "udp://tracker.example.com:6969/announce"}
	completedC := make(chan struct{})
	close(completedC)
	newPeers := make(chan []*net.TCPAddr, 1)
	getTorrent := func() tracker.Torrent {
		return tracker.Torrent{
			Port: 12345,
		}
	}
	an := NewPeriodicalAnnouncer(trk, 50, time.Minute, getTorrent, completedC, newPeers, logger.New("test"))

	// Run should exit on its own after receiving ErrUDPDisabled,
	// rather than retrying with backoff.
	done := make(chan struct{})
	go func() {
		an.Run()
		close(done)
	}()

	select {
	case <-done:
		// Run exited — announcer gave up as expected.
	case <-time.After(5 * time.Second):
		an.Close()
		t.Fatal("announcer did not stop after receiving ErrUDPDisabled")
	}

	// After Run exits, verify the stored state directly (Stats() requires
	// Run to be active, so we check the exported fields instead).
	if an.status != Stopped {
		t.Errorf("expected status Stopped (%d), got %d", Stopped, an.status)
	}
	if an.lastError == nil {
		t.Fatal("expected error in announcer state")
	}
	if an.lastError.Message != "udp trackers are disabled" {
		t.Errorf("expected error message %q, got %q", "udp trackers are disabled", an.lastError.Message)
	}
}
