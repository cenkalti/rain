package httptracker_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/tracker/httptracker"
	fhttp "github.com/chihaya/chihaya/frontend/http"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/storage"
	_ "github.com/chihaya/chihaya/storage/memory"
)

const timeout = 2 * time.Second

func trackerLogic(t *testing.T) *middleware.Logic {
	responseConfig := middleware.ResponseConfig{
		AnnounceInterval: time.Minute,
	}
	ps, err := storage.NewPeerStore("memory", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	return middleware.NewLogic(responseConfig, ps, nil, nil)
}

func startHTTPTracker(t *testing.T) (stop func()) {
	lgc := trackerLogic(t)
	fe, err := fhttp.NewFrontend(lgc, fhttp.Config{
		Addr:         "127.0.0.1:5000",
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
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

func TestHTTPTracker(t *testing.T) {
	defer startHTTPTracker(t)()

	const rawURL = "http://127.0.0.1:5000/announce"
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}

	trk := httptracker.New(rawURL, u, timeout, new(http.Transport), "Mozilla/5.0", 2*1024*1024)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Seeder
	req := tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			InfoHash:  [20]byte{6},
			PeerID:    [20]byte{1},
			Port:      1111,
			BytesLeft: 0,
		},
	}
	_, err = trk.Announce(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// Leecher
	req = tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			InfoHash:  [20]byte{6},
			PeerID:    [20]byte{2},
			Port:      2222,
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
