package torrent

import (
	"net"
	"sort"
	"time"
)

type peerAddr struct {
	*net.TCPAddr
	timestamp time.Time
}

func (t *Torrent) nextPeerAddr() net.Addr {
	if len(t.peerAddrs) == 0 {
		return nil
	}
	var p *peerAddr
	p, t.peerAddrs = t.peerAddrs[len(t.peerAddrs)-1], t.peerAddrs[:len(t.peerAddrs)-1] // pop back
	delete(t.peerAddrsMap, p.String())
	return p
}

func (t *Torrent) putPeerAddrs(addrs []*net.TCPAddr) {
	t.m.Lock()
	defer t.gotPeer.Signal()
	defer t.m.Unlock()
	now := time.Now()
	for _, a := range addrs {
		// 0 port is invalid
		if a.Port == 0 {
			continue
		}
		// Discard own client
		if a.IP.IsLoopback() && a.Port == t.Port() {
			continue
		}
		key := a.String()
		if p, ok := t.peerAddrsMap[key]; ok {
			p.timestamp = now
		} else {
			p = &peerAddr{
				TCPAddr:   a,
				timestamp: now,
			}
			t.peerAddrsMap[key] = p
			t.peerAddrs = append(t.peerAddrs, p)
		}
	}
	sort.Slice(t.peerAddrs, func(i, j int) bool { return t.peerAddrs[i].timestamp.Before(t.peerAddrs[j].timestamp) })
}
