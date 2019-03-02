package announcer

import (
	"context"
	"net"

	"github.com/cenkalti/rain/internal/tracker"
)

type cancelableAnnouncer struct {
	ResponseC  chan *tracker.AnnounceResponse
	ErrorC     chan error
	getTorrent func() tracker.Torrent
	newPeers   chan []*net.TCPAddr
	tracker    tracker.Tracker
	announcing bool
	stopC      chan struct{}
	doneC      chan struct{}
}

func newCancelableAnnouncer(trk tracker.Tracker, getTorrent func() tracker.Torrent, newPeers chan []*net.TCPAddr) *cancelableAnnouncer {
	return &cancelableAnnouncer{
		tracker:    trk,
		getTorrent: getTorrent,
		newPeers:   newPeers,
		ResponseC:  make(chan *tracker.AnnounceResponse),
		ErrorC:     make(chan error),
	}
}

func (a *cancelableAnnouncer) Announce(e tracker.Event, numWant int) {
	a.Cancel()
	a.announcing = true
	a.stopC = make(chan struct{})
	a.doneC = make(chan struct{})
	torrent := a.getTorrent() // get latests stats
	go announce(a.tracker, e, numWant, torrent, a.newPeers, a.ResponseC, a.ErrorC, a.stopC, a.doneC)
}

func (a *cancelableAnnouncer) Cancel() {
	if a.announcing {
		close(a.stopC)
		<-a.doneC
		a.announcing = false
	}
}

func announce(
	trk tracker.Tracker,
	e tracker.Event,
	numWant int,
	torrent tracker.Torrent,
	newPeers chan []*net.TCPAddr,
	responseC chan *tracker.AnnounceResponse,
	errC chan error,
	stopC, doneC chan struct{},
) {
	defer close(doneC)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-doneC:
		case <-stopC:
			cancel()
		}
	}()

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
		case <-stopC:
		}
		return
	}
	select {
	case newPeers <- annResp.Peers:
	case <-stopC:
	}
	select {
	case responseC <- annResp:
	case <-stopC:
	}
}
