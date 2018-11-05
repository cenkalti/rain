package announcer

import (
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/dht"
)

type DHTAnnouncer struct {
	lastAnnounce time.Time
	triggerC     chan struct{}
	closeC       chan struct{}
	doneC        chan struct{}
}

func NewDHTAnnouncer() *DHTAnnouncer {
	return &DHTAnnouncer{
		triggerC: make(chan struct{}),
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

func (a *DHTAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *DHTAnnouncer) Trigger() {
	select {
	case a.triggerC <- struct{}{}:
	default:
	}
}

func (a *DHTAnnouncer) Run(node dht.DHT, interval, minInterval time.Duration, newPeers chan []*net.TCPAddr, l logger.Logger) {
	defer close(a.doneC)

	peersC := node.Peers()
	var sendPeersC chan []*net.TCPAddr
	var peers []*net.TCPAddr

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.announce(node)
	for {
		select {
		case <-ticker.C:
			a.announce(node)
		case <-a.triggerC:
			if time.Since(a.lastAnnounce) > minInterval {
				a.announce(node)
			}
		case peers = <-peersC:
			sendPeersC = newPeers
		case sendPeersC <- peers:
			sendPeersC = nil
			peers = nil
		case <-a.closeC:
			return
		}
	}
}

func (a *DHTAnnouncer) announce(node dht.DHT) {
	node.Announce()
	a.lastAnnounce = time.Now()
}
