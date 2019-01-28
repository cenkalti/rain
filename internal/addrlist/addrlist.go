package addrlist

import (
	"net"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/blocklist"
	"github.com/cenkalti/rain/internal/externalip"
)

type PeerSource int

const (
	Tracker PeerSource = iota
	DHT
	PEX
	Manual
)

type AddrList struct {
	// Contains peers not connected yet, sorted by oldest first.
	peerAddrs []*peerAddr

	// Contains peers not connected yet, keyed by addr string
	peerAddrsMap map[string]*peerAddr

	maxItems   int
	listenPort int
	blocklist  *blocklist.Blocklist

	countBySource map[PeerSource]int
}

type peerAddr struct {
	*net.TCPAddr
	timestamp time.Time
	source    PeerSource
}

func New(maxItems int, blocklist *blocklist.Blocklist, listenPort int) *AddrList {
	return &AddrList{
		peerAddrsMap:  make(map[string]*peerAddr),
		maxItems:      maxItems,
		listenPort:    listenPort,
		blocklist:     blocklist,
		countBySource: make(map[PeerSource]int),
	}
}

func (d *AddrList) Reset() {
	d.peerAddrs = nil
	d.peerAddrsMap = make(map[string]*peerAddr)
	d.countBySource = make(map[PeerSource]int)
}

func (d *AddrList) Len() int {
	return len(d.peerAddrs)
}

func (d *AddrList) LenSource(s PeerSource) int {
	return d.countBySource[s]
}

func (d *AddrList) Pop() *net.TCPAddr {
	if len(d.peerAddrs) == 0 {
		return nil
	}
	p := d.peerAddrs[len(d.peerAddrs)-1]
	d.peerAddrs = d.peerAddrs[:len(d.peerAddrs)-1]
	delete(d.peerAddrsMap, p.TCPAddr.String())
	d.countBySource[p.source]--
	return p.TCPAddr
}

func (d *AddrList) Push(addrs []*net.TCPAddr, source PeerSource) {
	now := time.Now()
	var added int
	for _, ad := range addrs {
		// 0 port is invalid
		if ad.Port == 0 {
			continue
		}
		// Discard own client
		if ad.IP.IsLoopback() && ad.Port == d.listenPort {
			continue
		}
		if externalip.IsExternal(ad.IP) {
			continue
		}
		if d.blocklist != nil && d.blocklist.Blocked(ad.IP) {
			continue
		}
		key := ad.String()
		if p, ok := d.peerAddrsMap[key]; ok {
			p.timestamp = now
		} else {
			p = &peerAddr{
				TCPAddr:   ad,
				timestamp: now,
				source:    source,
			}
			d.peerAddrsMap[key] = p
			d.peerAddrs = append(d.peerAddrs, p)
			added++
		}
	}
	d.countBySource[source] += added
	sort.Slice(d.peerAddrs, func(i, j int) bool { return d.peerAddrs[i].timestamp.Before(d.peerAddrs[j].timestamp) })
	if len(d.peerAddrs) > d.maxItems {
		delta := len(d.peerAddrs) - d.maxItems
		for i := 0; i < delta; i++ {
			delete(d.peerAddrsMap, d.peerAddrs[i].String())
		}
		for i := 0; i < len(d.peerAddrs)-delta; i++ {
			d.peerAddrs[i] = d.peerAddrs[i+delta]
		}
		d.peerAddrs = d.peerAddrs[:len(d.peerAddrs)-delta]
		d.countBySource[source] -= delta
	}
}
