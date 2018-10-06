package addrlist

import (
	"net"
	"sort"
	"time"
)

type AddrList struct {
	// Contains peers not connected yet, sorted by oldest first.
	peerAddrs []*peerAddr

	// Contains peers not connected yet, keyed by addr string
	peerAddrsMap map[string]*peerAddr
}

type peerAddr struct {
	*net.TCPAddr
	timestamp time.Time
}

func New() *AddrList {
	return &AddrList{
		peerAddrsMap: make(map[string]*peerAddr),
	}
}

func (d *AddrList) Reset() {
	d.peerAddrs = nil
	d.peerAddrsMap = make(map[string]*peerAddr)
}

func (d *AddrList) Pop() net.Addr {
	if len(d.peerAddrs) == 0 {
		return nil
	}
	addr := d.peerAddrs[len(d.peerAddrs)-1].TCPAddr
	d.peerAddrs = d.peerAddrs[:len(d.peerAddrs)-1]
	delete(d.peerAddrsMap, addr.String())
	return addr
}

func (d *AddrList) Push(addrs []*net.TCPAddr, listenPort int) {
	now := time.Now()
	for _, ad := range addrs {
		// 0 port is invalid
		if ad.Port == 0 {
			continue
		}
		// Discard own client
		// TODO discard IP reported by Tracker
		if ad.IP.IsLoopback() && ad.Port == listenPort {
			continue
		}
		key := ad.String()
		if p, ok := d.peerAddrsMap[key]; ok {
			p.timestamp = now
		} else {
			p = &peerAddr{
				TCPAddr:   ad,
				timestamp: now,
			}
			d.peerAddrsMap[key] = p
			d.peerAddrs = append(d.peerAddrs, p)
		}
	}
	// TODO limit max peer addresses to keep
	sort.Slice(d.peerAddrs, func(i, j int) bool { return d.peerAddrs[i].timestamp.Before(d.peerAddrs[j].timestamp) })
}
