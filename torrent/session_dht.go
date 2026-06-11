package torrent

import (
	"net"
	"time"

	"github.com/cenkalti/rain/v2/internal/tracker"
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
		// DHT returns one entry per peer. UnmarshalBinary rejects anything
		// that isn't a 6-byte compact peer, so non-IPv4 entries are skipped.
		var cp tracker.CompactPeer
		if cp.UnmarshalBinary([]byte(peer)) != nil {
			continue
		}
		addrs = append(addrs, cp.Addr())
	}
	return addrs
}
