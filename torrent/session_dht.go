package torrent

import (
	"net"
	"time"
)

func (s *Session) processDHTResults() {
	dhtLimiter := time.NewTicker(time.Second)
	defer dhtLimiter.Stop()
	for {
		select {
		case <-dhtLimiter.C:
			s.handleDHTtick()
		case res := <-s.dht.PeersRequestResults:
			for ih, peers := range res {
				s.mTorrents.RLock()
				torrents, ok := s.torrentsByInfoHash[ih]
				s.mTorrents.RUnlock()
				if !ok {
					continue
				}
				addrs := parseDHTPeers(peers)
				for _, t := range torrents {
					select {
					case t.torrent.dhtPeersC <- addrs:
					case <-t.torrent.closeC:
					default:
					}
				}
			}
		case <-s.closeC:
			return
		}
	}
}

func (s *Session) handleDHTtick() {
	s.mPeerRequests.Lock()
	defer s.mPeerRequests.Unlock()
	for t := range s.dhtPeerRequests {
		s.dht.PeersRequestPort(string(t.infoHash[:]), true, t.port)
		delete(s.dhtPeerRequests, t)
		return
	}
}

func parseDHTPeers(peers []string) []*net.TCPAddr {
	addrs := make([]*net.TCPAddr, 0, len(peers))
	for _, peer := range peers {
		if len(peer) != 6 {
			// only IPv4 is supported for now
			continue
		}
		addr := &net.TCPAddr{
			IP:   net.IP(peer[:4]),
			Port: int((uint16(peer[4]) << 8) | uint16(peer[5])),
		}
		addrs = append(addrs, addr)
	}
	return addrs
}
