package httptracker_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/tracker/httptracker"
	fhttp "github.com/chihaya/chihaya/frontend/http"
	"github.com/chihaya/chihaya/middleware"
	"github.com/chihaya/chihaya/storage"
	_ "github.com/chihaya/chihaya/storage/memory"
)

const timeout = 2 * time.Second

func trackerLogic(t *testing.T) *middleware.Logic {
	responseConfig := middleware.Config{
		AnnounceInterval:    time.Minute,
		MaxNumWant:          200,
		DefaultNumWant:      50,
		MaxScrapeInfoHashes: 400,
	}
	ps, err := storage.NewPeerStore("memory", map[string]interface{}{
		"gc_interval":                   3 * time.Minute,
		"peer_lifetime":                 31 * time.Minute,
		"shard_count":                   1024,
		"prometheus_reporting_interval": time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return middleware.NewLogic(responseConfig, ps, nil, nil)
}

func startHTTPTracker(t *testing.T) (stop func()) {
	lgc := trackerLogic(t)
	httpfe, err := fhttp.NewFrontend(lgc, fhttp.Config{
		Addr:         "127.0.0.1:5000",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		errC := httpfe.Stop()
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

	trk := httptracker.New(rawURL, u, timeout, new(http.Transport), "Mozilla/5.0")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req := tracker.AnnounceRequest{
		Torrent: tracker.Torrent{
			Port:   1111,
			PeerID: [20]byte{1},
		},
		Event: tracker.EventCompleted,
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
		Event: tracker.EventStarted,
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
