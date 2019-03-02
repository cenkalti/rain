package announcer

import (
	"context"

	"github.com/cenkalti/rain/internal/tracker"
)

func announce(
	ctx context.Context,
	trk tracker.Tracker,
	e tracker.Event,
	numWant int,
	torrent tracker.Torrent,
	responseC chan *tracker.AnnounceResponse,
	errC chan error,
) {
	annReq := tracker.AnnounceRequest{
		Torrent: torrent,
		Event:   e,
		NumWant: numWant,
	}
	annResp, err := trk.Announce(ctx, annReq)
	if err == context.Canceled {
		return
	}
	if err != nil {
		select {
		case errC <- err:
		case <-ctx.Done():
		}
		return
	}
	select {
	case responseC <- annResp:
	case <-ctx.Done():
	}
}
