package torrent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	cp "github.com/otiai10/copy"
)

func TestCustomDialFuncNoLeak(t *testing.T) {
	// Start an HTTP tracker so the torrent has something to announce to.
	startHTTPTracker(t)

	// Set up a seeder with a normal session (no custom dial) that has the data.
	seederAddr := seeder(t, false)

	// Track all connections made through the custom dial function.
	var mu sync.Mutex
	var dialedAddrs []string
	customDial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialedAddrs = append(dialedAddrs, addr)
		mu.Unlock()
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}

	// Create a leecher session with CustomDialFunc and UDP trackers disabled.
	tmp := t.TempDir()
	cfg := DefaultConfig
	cfg.Database = filepath.Join(tmp, "session.db")
	cfg.DataDir = tmp
	cfg.DHTEnabled = false
	cfg.PEXEnabled = false
	cfg.RPCEnabled = false
	cfg.Host = "127.0.0.1"
	cfg.CustomDialFunc = customDial
	cfg.DisableUDPTrackers = true

	s, err := NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})

	// Add the torrent and start downloading.
	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	tor, err := s.AddTorrent(f, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Also add the seeder as a direct peer so the download can complete.
	tor.AddPeer(seederAddr)

	assertCompleted(t, tor)

	mu.Lock()
	addrs := make([]string, len(dialedAddrs))
	copy(addrs, dialedAddrs)
	mu.Unlock()

	if len(addrs) == 0 {
		t.Fatal("custom dial function was never called — all connections bypassed it")
	}

	// Verify that every dialed address is a loopback address (127.0.0.1).
	// In this test, the only reachable services are on localhost (tracker + seeder).
	// If any connection went to a non-loopback address, something leaked.
	for _, addr := range addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			t.Errorf("unexpected address format: %s", addr)
			continue
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			t.Errorf("connection to non-loopback address bypassed custom dial: %s", addr)
		}
	}

	t.Logf("custom dial handled %d connections", len(addrs))
}

func TestDisabledUDPTrackerStatus(t *testing.T) {
	// Set up a seeder with data so the torrent can start its event loop.
	seederAddr := seeder(t, true)

	// Create a session with UDP trackers disabled.
	tmp := t.TempDir()
	cfg := DefaultConfig
	cfg.Database = filepath.Join(tmp, "session.db")
	cfg.DataDir = tmp
	cfg.DHTEnabled = false
	cfg.PEXEnabled = false
	cfg.RPCEnabled = false
	cfg.Host = "127.0.0.1"
	cfg.DisableUDPTrackers = true

	s, err := NewSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})

	// Add torrent stopped, then inject a UDP tracker URL and start.
	f, err := os.Open(torrentFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	opt := &AddTorrentOptions{Stopped: true}
	tor, err := s.AddTorrent(f, opt)
	if err != nil {
		t.Fatal(err)
	}

	// Copy the data so the torrent has something to seed (prevents error state).
	src := filepath.Join(torrentDataDir, torrentName)
	dst := filepath.Join(s.config.DataDir, tor.ID(), torrentName)
	err = os.Mkdir(filepath.Join(s.config.DataDir, tor.ID()), os.ModeDir|s.config.FilePermissions)
	if err != nil {
		t.Fatal(err)
	}
	err = cp.Copy(src, dst)
	if err != nil {
		t.Fatal(err)
	}

	// Replace trackers with a UDP-only tracker URL.
	tor.torrent.trackers = s.parseTrackers([][]string{{"udp://tracker.example.com:6969/announce"}}, false)

	tor.Start()
	tor.AddPeer(seederAddr)

	// Wait for the torrent to start and the announcer to process.
	waitForStart(t, tor)

	// Give the announcer time to attempt the announce and stop.
	time.Sleep(500 * time.Millisecond)

	trackers := tor.Trackers()
	if len(trackers) == 0 {
		t.Fatal("expected at least one tracker")
	}

	found := false
	for _, tr := range trackers {
		if tr.URL == "udp://tracker.example.com:6969/announce" {
			found = true
			if tr.Status != TrackerStopped {
				t.Errorf("expected tracker status TrackerStopped (%d), got %d", TrackerStopped, tr.Status)
			}
			if tr.Error == nil {
				t.Error("expected error on disabled tracker")
			} else if tr.Error.Error() != "udp trackers are disabled" {
				t.Errorf("expected error %q, got %q", "udp trackers are disabled", tr.Error.Error())
			}
		}
	}
	if !found {
		t.Error("UDP tracker not found in tracker list")
	}
}
