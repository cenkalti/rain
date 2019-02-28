package torrent

import (
	"sort"

	"github.com/cenkalti/rain/internal/peer"
)

func (t *torrent) candidatesUnchoke() []*peer.Peer {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if pe.PeerInterested && !pe.OptimisticUnchoked {
			peers = append(peers, pe)
		}
	}
	return peers
}

func (t *torrent) candidatesUnchokeOptimistic() []*peer.Peer {
	peers := make([]*peer.Peer, 0, len(t.peers))
	for pe := range t.peers {
		if pe.PeerInterested && pe.ClientChoking {
			peers = append(peers, pe)
		}
	}
	return peers
}

func (t *torrent) tickUnchoke() {
	peers := t.candidatesUnchoke()
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
			t.unchokePeer(pe, false)
			unchoked++
		} else {
			t.chokePeer(pe)
		}
	}
}

func (t *torrent) tickOptimisticUnchoke() {
	peers := t.candidatesUnchokeOptimistic()
	var unchoked int
	for _, pe := range peers {
		if unchoked < t.config.OptimisticUnchokedPeers {
			t.unchokePeer(pe, true)
			unchoked++
		} else {
			t.chokePeer(pe)
		}
	}
}
