package udptracker_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/cenkalti/rain/v2/internal/tracker"
	"github.com/cenkalti/rain/v2/internal/tracker/udptracker"
	"github.com/chihaya/chihaya/frontend/udp"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/storage"
	_ "github.com/chihaya/chihaya/storage/memory"
)

const timeout = 2 * time.Second

func trackerLogic(t *testing.T) *middleware.Logic {
	responseConfig := middleware.ResponseConfig{
		AnnounceInterval: time.Minute,
	}
	ps, err := storage.NewPeerStore("memory", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	return middleware.NewLogic(responseConfig, ps, nil, nil)
}

func startUDPTracker(t *testing.T, port int) func() {
	lgc := trackerLogic(t)
	fe, err := udp.NewFrontend(lgc, udp.Config{
		Addr:         "127.0.0.1:" + strconv.Itoa(port),
		MaxClockSkew: time.Minute,
		PrivateKey:   "M4YlzP02iB0B46P2i3QLyMOW6nWXnVlYeJ91xIdtu8Ao7IIVKLZEaCEshTChmFrS",
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		errC := fe.Stop()
		err := <-errC
		if err != nil {
			t.Fatal(err)
		}
	}
}

// freeUDPPort returns a UDP port that is free at the time of the call. Picking
// a port dynamically instead of hardcoding one avoids collisions with other
// processes (or parallel test packages) on the host.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func containsPort(peers []*net.TCPAddr, port int) bool {
	for _, p := range peers {
		if p.Port == port {
			return true
		}
	}
	return false
}

func TestUDPTracker(t *testing.T) {
	const seederPort = 1111

	port := freeUDPPort(t)
	defer startUDPTracker(t, port)()

	rawURL := fmt.Sprintf("udp://127.0.0.1:%d/announce", port)
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	tr := udptracker.NewTransport(nil, 5*time.Second)
	go tr.Run()
	defer tr.Close()
	trk := udptracker.New(rawURL, u, tr)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Register a seeder (BytesLeft == 0).
	seeder := tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			Port:   seederPort,
			PeerID: [20]byte{1},
		},
	}
	if _, err := trk.Announce(ctx, seeder); err != nil {
		t.Fatal(err)
	}

	// A leecher announcing the same torrent should receive the seeder in its
	// peer list. The tracker builds the response from swarm state as it was
	// *before* the announcing peer is stored, and UDP retransmits mean the
	// exact set returned by any single announce is not deterministic, so retry
	// until the seeder shows up or the context deadline is reached.
	leecher := tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			Port:      2222,
			PeerID:    [20]byte{2},
			BytesLeft: 1,
		},
		NumWant: 10,
	}
	for {
		resp, err := trk.Announce(ctx, leecher)
		if err != nil {
			t.Fatalf("leecher announce failed: %v", err)
		}
		if containsPort(resp.Peers, seederPort) {
			return // success: leecher received the seeder
		}
		select {
		case <-ctx.Done():
			t.Fatalf("seeder (port %d) not returned to leecher; last peers: %v", seederPort, resp.Peers)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
