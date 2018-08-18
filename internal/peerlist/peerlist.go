package peerlist

import (
	"net"
	"sort"
	"time"
)

type PeerList struct {
	// Peers sent to this channel will returned when Get is called.
	NewPeers     chan []*net.TCPAddr
	peerAddrs    []*peerAddr          // contains peers not connected yet, sorted by oldest first
	peerAddrsMap map[string]*peerAddr // contains peers not connected yet, keyed by addr string
	getC         chan *net.TCPAddr
}

type peerAddr struct {
	*net.TCPAddr
	timestamp time.Time
}

func New() *PeerList {
	return &PeerList{
		peerAddrsMap: make(map[string]*peerAddr),
		NewPeers:     make(chan []*net.TCPAddr),
		getC:         make(chan *net.TCPAddr),
	}
}

func (a *PeerList) Run(stopC chan struct{}) {
	for {
		if len(a.peerAddrs) == 0 {
			select {
			case addrs := <-a.NewPeers:
				a.put(addrs)
				continue
			case <-stopC:
				close(a.getC)
				return
			}
		}
		addr := a.peerAddrs[len(a.peerAddrs)-1].TCPAddr
		select {
		case addrs := <-a.NewPeers:
			a.put(addrs)
		case a.getC <- addr:
			a.peerAddrs = a.peerAddrs[:len(a.peerAddrs)-1]
			delete(a.peerAddrsMap, addr.String())
		case <-stopC:
			close(a.getC)
			return
		}
	}
}

// Get returns the next peer address to connect from the list.
func (a *PeerList) Get() <-chan net.Addr {
	ret := make(chan net.Addr, 1)
	go func() {
		addr := <-a.getC
		if addr == nil {
			return
		}
		ret <- addr
	}()
	return ret
}

func (a *PeerList) put(addrs []*net.TCPAddr) {
	now := time.Now()
	for _, ad := range addrs {
		// 0 port is invalid
		if ad.Port == 0 {
			continue
		}
		// TODO Discard own client
		// if ad.IP.IsLoopback() && ad.Port == a.transfer.Port() {
		// 	continue
		// }
		key := ad.String()
		if p, ok := a.peerAddrsMap[key]; ok {
			p.timestamp = now
		} else {
			p = &peerAddr{
				TCPAddr:   ad,
				timestamp: now,
			}
			a.peerAddrsMap[key] = p
			a.peerAddrs = append(a.peerAddrs, p)
		}
	}
	sort.Slice(a.peerAddrs, func(i, j int) bool { return a.peerAddrs[i].timestamp.Before(a.peerAddrs[j].timestamp) })
}
