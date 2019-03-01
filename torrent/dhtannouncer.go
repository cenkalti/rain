package torrent

import (
	"net"
	"sync"
)

type dhtAnnouncer struct {
	infoHash string
	port     int
	peersC   chan []*net.TCPAddr

	mRequests *sync.Mutex
	requests  map[*dhtAnnouncer]struct{}
}

func newDHTAnnouncer(infoHash []byte, port int, requests map[*dhtAnnouncer]struct{}, mRequests *sync.Mutex) *dhtAnnouncer {
	return &dhtAnnouncer{
		infoHash:  string(infoHash),
		port:      port,
		peersC:    make(chan []*net.TCPAddr, 1),
		mRequests: mRequests,
		requests:  requests,
	}
}

func (a *dhtAnnouncer) Announce() {
	a.mRequests.Lock()
	a.requests[a] = struct{}{}
	a.mRequests.Unlock()
}

func (a *dhtAnnouncer) Peers() chan []*net.TCPAddr {
	return a.peersC
}
