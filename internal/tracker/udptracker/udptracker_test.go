package udptracker_test

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/udptracker"
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

func TestUDPTracker(t *testing.T) {
	defer startUDPTracker(t, 5000)()

	const rawURL = "udp://127.0.0.1:5000/announce"
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

	req := tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			Port:   1111,
			PeerID: [20]byte{1},
		},
	}
	_, err = trk.Announce(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	req = tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			Port:      2222,
			PeerID:    [20]byte{2},
			BytesLeft: 1,
		},
		NumWant: 10,
	}
	resp, err := trk.Announce(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Peers) != 1 {
		t.Logf("%#v", resp)
		t.FailNow()
	}
	addr := resp.Peers[0]
	if addr.Port != 1111 {
		t.Log(addr.String())
		t.FailNow()
	}
}
