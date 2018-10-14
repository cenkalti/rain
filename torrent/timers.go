package torrent

import (
	"math/rand"
	"sort"

	"github.com/cenkalti/rain/torrent/internal/peer"
)

func (t *Torrent) tickUnchoke() {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if !pe.OptimisticUnchoked {
			peers = append(peers, pe)
		}
	}
	sort.Sort(peer.ByDownloadRate(peers))
	for pe := range t.peers {
		pe.BytesDownlaodedInChokePeriod = 0
	}
	unchokedPeers := make(map[*peer.Peer]struct{}, 3)
	for i, pe := range peers {
		if i == 3 {
			break
		}
		t.unchokePeer(pe)
		unchokedPeers[pe] = struct{}{}
	}
	for pe := range t.peers {
		if _, ok := unchokedPeers[pe]; !ok {
			t.chokePeer(pe)
		}
	}
}

func (t *Torrent) tickOptimisticUnchoke() {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if !pe.OptimisticUnchoked && pe.AmChoking {
			peers = append(peers, pe)
		}
	}
	if t.optimisticUnchokedPeer != nil {
		t.optimisticUnchokedPeer.OptimisticUnchoked = false
		t.chokePeer(t.optimisticUnchokedPeer)
	}
	if len(peers) == 0 {
		t.optimisticUnchokedPeer = nil
		return
	}
	pe := peers[rand.Intn(len(peers))]
	pe.OptimisticUnchoked = true
	t.unchokePeer(pe)
	t.optimisticUnchokedPeer = pe
}
