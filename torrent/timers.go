package torrent

import (
	"math/rand"
	"sort"

	"github.com/cenkalti/rain/internal/peer"
)

func (t *torrent) peerList() []*peer.Peer {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if pe.PeerInterested && !pe.OptimisticUnchoked {
			peers = append(peers, pe)
		}
	}
	return peers
}

func (t *torrent) tickUnchoke() {
	peers := t.peerList()
	if t.completed {
		sort.Slice(peers, func(i, j int) bool {
			return peers[i].UploadSpeed() > peers[j].UploadSpeed()
		})
	} else {
		sort.Slice(peers, func(i, j int) bool {
			return peers[i].DownloadSpeed() > peers[j].DownloadSpeed()
		})
	}
	var unchoked int
	for _, pe := range peers {
		if unchoked < t.config.UnchokedPeers {
			t.unchokePeer(pe)
			unchoked++
			// Set optimistic flag false, so optimistic timer don't choke this peer
			// because we have selected it based it's good download rate.
			pe.OptimisticUnchoked = false
		} else {
			t.chokePeer(pe)
		}
	}
}

func (t *torrent) tickOptimisticUnchoke() {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if pe.PeerInterested && !pe.OptimisticUnchoked && pe.ClientChoking {
			peers = append(peers, pe)
		}
	}

	// Choke previously optimistic unchoked peers.
	for _, pe := range t.optimisticUnchokedPeers {
		if pe.OptimisticUnchoked {
			t.chokePeer(pe)
		}
	}
	t.optimisticUnchokedPeers = t.optimisticUnchokedPeers[:0]

	for i := 0; i < t.config.OptimisticUnchokedPeers; i++ {
		if len(peers) == 0 {
			break
		}
		pe := peers[rand.Intn(len(peers))]
		pe.OptimisticUnchoked = true
		t.unchokePeer(pe)
		t.optimisticUnchokedPeers = append(t.optimisticUnchokedPeers, pe)
	}
}
