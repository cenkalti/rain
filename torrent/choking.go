package torrent

import (
	"sort"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerprotocol"
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
		if pe.PeerInterested && pe.ClientChoking && pe.OptimisticUnchoked {
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

func (t *torrent) fastUnchoke(pe *peer.Peer) {
	if pe.ClientChoking && pe.PeerInterested && len(t.peersUnchoked) < t.config.UnchokedPeers {
		t.unchokePeer(pe, false)
	}
	if pe.ClientChoking && pe.PeerInterested && len(t.peersUnchokedOptimistic) < t.config.OptimisticUnchokedPeers {
		t.unchokePeer(pe, true)
	}
}

func (t *torrent) chokePeer(pe *peer.Peer) {
	if !pe.ClientChoking {
		pe.ClientChoking = true
		pe.OptimisticUnchoked = false
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg)
		delete(t.peersUnchoked, pe)
		delete(t.peersUnchokedOptimistic, pe)
	}
}

func (t *torrent) unchokePeer(pe *peer.Peer, optimistic bool) {
	if pe.ClientChoking {
		pe.ClientChoking = false
		pe.OptimisticUnchoked = optimistic
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg)
		if optimistic {
			t.peersUnchokedOptimistic[pe] = struct{}{}
		} else {
			t.peersUnchoked[pe] = struct{}{}
		}
	}
}
