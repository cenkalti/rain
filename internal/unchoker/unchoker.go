package unchoker

import (
	"math/rand"
	"sort"
)

type Unchoker struct {
	getPeers              func() []Peer
	torrentCompleted      *bool
	numUnchoked           int
	numOptimisticUnchoked int

	// Every 3rd round an optimistic unchoke logic is applied.
	round uint8

	peersUnchoked           map[Peer]struct{}
	peersUnchokedOptimistic map[Peer]struct{}
}

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

	DownloadSpeed() uint
	UploadSpeed() uint
}

func New(getPeers func() []Peer, torrentCompleted *bool, numUnchoked, numOptimisticUnchoked int) *Unchoker {
	return &Unchoker{
		getPeers:                getPeers,
		torrentCompleted:        torrentCompleted,
		numUnchoked:             numUnchoked,
		numOptimisticUnchoked:   numOptimisticUnchoked,
		peersUnchoked:           make(map[Peer]struct{}, numUnchoked),
		peersUnchokedOptimistic: make(map[Peer]struct{}, numUnchoked),
	}
}

func (u *Unchoker) HandleDisconnect(pe Peer) {
	delete(u.peersUnchoked, pe)
	delete(u.peersUnchokedOptimistic, pe)
}

func (u *Unchoker) candidatesUnchoke() []Peer {
	allPeers := u.getPeers()
	peers := make([]Peer, 0, len(allPeers))
	for _, pe := range allPeers {
		if pe.Interested() {
			peers = append(peers, pe)
		}
	}
	return peers
}

func (u *Unchoker) sortPeers(peers []Peer) {
	byUploadSpeed := func(i, j int) bool { return peers[i].UploadSpeed() > peers[j].UploadSpeed() }
	byDownloadSpeed := func(i, j int) bool { return peers[i].DownloadSpeed() > peers[j].DownloadSpeed() }
	if *u.torrentCompleted {
		sort.Slice(peers, byUploadSpeed)
	} else {
		sort.Slice(peers, byDownloadSpeed)
	}
}

// TickUnchoke must be called at every 10 seconds.
func (u *Unchoker) TickUnchoke() {
	optimistic := u.round == 0
	peers := u.candidatesUnchoke()
	u.sortPeers(peers)
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
			n := rand.Intn(len(peers))
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

func (u *Unchoker) FastUnchoke(pe Peer) {
	if pe.Choking() && pe.Interested() && len(u.peersUnchoked) < u.numUnchoked {
		u.unchokePeer(pe)
	}
	if pe.Choking() && pe.Interested() && len(u.peersUnchokedOptimistic) < u.numOptimisticUnchoked {
		u.optimisticUnchokePeer(pe)
	}
}
