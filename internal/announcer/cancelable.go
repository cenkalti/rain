package announcer

import (
	"context"
	"net"

	"github.com/cenkalti/rain/internal/tracker"
)

type cancelableAnnouncer struct {
	ResponseC  chan *tracker.AnnounceResponse
	ErrorC     chan error
	requestC   chan *Request
	newPeers   chan []*net.TCPAddr
	tracker    tracker.Tracker
	announcing bool
	stopC      chan struct{}
	doneC      chan struct{}
}

func newCancelableAnnouncer(trk tracker.Tracker, requestC chan *Request, newPeers chan []*net.TCPAddr) *cancelableAnnouncer {
	return &cancelableAnnouncer{
		tracker:   trk,
		requestC:  requestC,
		newPeers:  newPeers,
		ResponseC: make(chan *tracker.AnnounceResponse),
		ErrorC:    make(chan error),
	}
}

func (a *cancelableAnnouncer) Announce(e tracker.Event, numWant int) {
	a.Cancel()
	a.announcing = true
	a.stopC = make(chan struct{})
	a.doneC = make(chan struct{})
	go announce(a.tracker, e, numWant, a.requestC, a.newPeers, a.ResponseC, a.ErrorC, a.stopC, a.doneC)
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
	requestC chan *Request,
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

	req := &Request{
		Response: make(chan Response),
		Cancel:   make(chan struct{}),
	}
	defer close(req.Cancel)

	select {
	case requestC <- req:
	case <-stopC:
		return
	}

	var resp Response
	select {
	case resp = <-req.Response:
	case <-stopC:
		return
	}

	annReq := tracker.AnnounceRequest{
		Torrent: resp.Torrent,
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
