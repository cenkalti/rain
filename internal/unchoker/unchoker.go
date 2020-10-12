package unchoker

import (
	"math/rand"
	"sort"
)

// Unchoker implements an algorithm to select peers to unchoke based on their download speed.
type Unchoker struct {
	numUnchoked           int
	numOptimisticUnchoked int

	// Every 3rd round an optimistic unchoke logic is applied.
	round uint8

	peersUnchoked           map[Peer]struct{}
	peersUnchokedOptimistic map[Peer]struct{}
}

// Peer of a torrent.
type Peer interface {
	// Sends messages and set choking status of local peeer
	Choke()
	Unchoke()

	// Choking returns choke status of local peer
	Choking() bool

	// Interested returns interest status of remote peer
	Interested() bool

	// SetOptimistic sets the uptimistic unchoke status of peer
	SetOptimistic(value bool)
	// OptimisticUnchoked returns the value previously set by SetOptimistic
	Optimistic() bool

	DownloadSpeed() int
	UploadSpeed() int
}

// New returns a new Unchoker.
func New(numUnchoked, numOptimisticUnchoked int) *Unchoker {
	return &Unchoker{
		numUnchoked:             numUnchoked,
		numOptimisticUnchoked:   numOptimisticUnchoked,
		peersUnchoked:           make(map[Peer]struct{}, numUnchoked),
		peersUnchokedOptimistic: make(map[Peer]struct{}, numUnchoked),
	}
}

// HandleDisconnect must be called to remove the peer from internal indexes.
func (u *Unchoker) HandleDisconnect(pe Peer) {
	delete(u.peersUnchoked, pe)
	delete(u.peersUnchokedOptimistic, pe)
}

func (u *Unchoker) candidatesUnchoke(allPeers []Peer) []Peer {
	peers := allPeers[:0]
	for _, pe := range allPeers {
		if pe.Interested() {
			peers = append(peers, pe)
		}
	}
	return peers
}

func (u *Unchoker) sortPeers(peers []Peer, completed bool) {
	byUploadSpeed := func(i, j int) bool { return peers[i].UploadSpeed() > peers[j].UploadSpeed() }
	byDownloadSpeed := func(i, j int) bool { return peers[i].DownloadSpeed() > peers[j].DownloadSpeed() }
	if completed {
		sort.Slice(peers, byUploadSpeed)
	} else {
		sort.Slice(peers, byDownloadSpeed)
	}
}

// TickUnchoke must be called at every 10 seconds.
func (u *Unchoker) TickUnchoke(allPeers []Peer, torrentCompleted bool) {
	optimistic := u.round == 0
	peers := u.candidatesUnchoke(allPeers)
	u.sortPeers(peers, torrentCompleted)
	var i, unchoked int
	for ; i < len(peers) && unchoked < u.numUnchoked; i++ {
		if !optimistic && peers[i].Optimistic() {
			continue
		}
		u.unchokePeer(peers[i])
		unchoked++
	}
	peers = peers[i:]
	if optimistic {
		for i = 0; i < u.numOptimisticUnchoked && len(peers) > 0; i++ {
			n := rand.Intn(len(peers)) // nolint: gosec
			pe := peers[n]
			u.optimisticUnchokePeer(pe)
			peers[n], peers = peers[len(peers)-1], peers[:len(peers)-1]
		}
	}
	for _, pe := range peers {
		u.chokePeer(pe)
	}
	u.round = (u.round + 1) % 3
}

func (u *Unchoker) chokePeer(pe Peer) {
	if pe.Choking() {
		return
	}
	pe.Choke()
	pe.SetOptimistic(false)
	delete(u.peersUnchoked, pe)
	delete(u.peersUnchokedOptimistic, pe)
}

func (u *Unchoker) unchokePeer(pe Peer) {
	if !pe.Choking() {
		if pe.Optimistic() {
			// Move into regular unchoked peers
			pe.SetOptimistic(false)
			delete(u.peersUnchokedOptimistic, pe)
			u.peersUnchoked[pe] = struct{}{}
		}
		return
	}
	pe.Unchoke()
	u.peersUnchoked[pe] = struct{}{}
	pe.SetOptimistic(false)
}

func (u *Unchoker) optimisticUnchokePeer(pe Peer) {
	if !pe.Choking() {
		if !pe.Optimistic() {
			// Move into optimistic unchoked peers
			pe.SetOptimistic(true)
			delete(u.peersUnchoked, pe)
			u.peersUnchokedOptimistic[pe] = struct{}{}
		}
		return
	}
	pe.Unchoke()
	u.peersUnchokedOptimistic[pe] = struct{}{}
	pe.SetOptimistic(true)
}

// FastUnchoke must be called when remote peer is interested.
// Remote peer is unchoked immediately if there are not enough unchoked peers.
// Without this function, remote peer would have to wait for next unchoke period.
func (u *Unchoker) FastUnchoke(pe Peer) {
	if pe.Choking() && pe.Interested() && len(u.peersUnchoked) < u.numUnchoked {
		u.unchokePeer(pe)
	}
	if pe.Choking() && pe.Interested() && len(u.peersUnchokedOptimistic) < u.numOptimisticUnchoked {
		u.optimisticUnchokePeer(pe)
	}
}
