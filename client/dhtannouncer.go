package client

import (
	"net"

	"github.com/cenkalti/rain/torrent/dht"
	node "github.com/nictuku/dht"
)

type dhtAnnouncer struct {
	node     *node.DHT
	infoHash string
	peersC   chan []*net.TCPAddr
}

var _ dht.DHT = (*dhtAnnouncer)(nil)

func newDHTAnnouncer(node *node.DHT, infoHash []byte) *dhtAnnouncer {
	return &dhtAnnouncer{
		node:     node,
		infoHash: string(infoHash),
		peersC:   make(chan []*net.TCPAddr),
	}
}

func (a *dhtAnnouncer) Announce() {
	a.node.PeersRequest(a.infoHash, true)
}

func (a *dhtAnnouncer) Peers() chan []*net.TCPAddr {
	return a.peersC
}
