package addrlist

import (
	"net"

	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/externalip"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerpriority"
	"github.com/google/btree"
)

// AddrList contains peer addresses that are ready to be connected.
type AddrList struct {
	peers         *btree.BTree
	maxItems      int
	listenPort    int
	clientIP      *net.IP
	blocklist     *blocklist.Blocklist
	countBySource map[peer.Source]int
}

func New(maxItems int, blocklist *blocklist.Blocklist, listenPort int, clientIP *net.IP) *AddrList {
	return &AddrList{
		peers: btree.New(2),

		maxItems:      maxItems,
		listenPort:    listenPort,
		clientIP:      clientIP,
		blocklist:     blocklist,
		countBySource: make(map[peer.Source]int),
	}
}

func (d *AddrList) Reset() {
	d.peers.Clear(false)
	d.countBySource = make(map[peer.Source]int)
}

func (d *AddrList) Len() int {
	return d.peers.Len()
}

func (d *AddrList) LenSource(s peer.Source) int {
	return d.countBySource[s]
}

func (d *AddrList) Pop() (*net.TCPAddr, peer.Source) {
	item := d.peers.DeleteMax()
	if item == nil {
		return nil, 0
	}
	p := item.(*peerAddr)
	d.countBySource[p.source]--
	return p.addr, p.source
}

func (d *AddrList) Push(addrs []*net.TCPAddr, source peer.Source) {
	var added int
	for _, ad := range addrs {
		// 0 port is invalid
		if ad.Port == 0 {
			continue
		}
		// Discard own client
		if ad.IP.IsLoopback() && ad.Port == d.listenPort {
			continue
		} else if d.clientIP.Equal(ad.IP) {
			continue
		}
		if externalip.IsExternal(ad.IP) {
			continue
		}
		if d.blocklist != nil && d.blocklist.Blocked(ad.IP) {
			continue
		}
		p := &peerAddr{
			addr:     ad,
			source:   source,
			priority: peerpriority.Calculate(ad, d.clientAddr()),
		}
		item := d.peers.ReplaceOrInsert(p)
		if item != nil {
			prev := item.(*peerAddr)
			d.countBySource[prev.source]--
		}
		added++
	}
	d.countBySource[source] += added

	delta := d.peers.Len() - d.maxItems
	if delta > 0 {
		d.removeExcessItems(delta)
		d.countBySource[source] -= delta
	}
}

func (d *AddrList) removeExcessItems(delta int) {
	for i := 0; i < delta; i++ {
		d.peers.DeleteMin()
	}
}

func (d *AddrList) clientAddr() *net.TCPAddr {
	ip := *d.clientIP
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}
	return &net.TCPAddr{
		IP:   ip,
		Port: d.listenPort,
	}
}
