package unchoker

import "sort"

type Unchoker struct {
	getPeers              func() []Peer
	torrentCompleted      *bool
	numUnchoked           int
	numOptimisticUnchoked int

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

func (u *Unchoker) candidatesUnchokeOptimistic() []Peer {
	allPeers := u.getPeers()
	peers := make([]Peer, 0, len(allPeers))
	for _, pe := range allPeers {
		if pe.Interested() && (pe.Choking() || pe.Optimistic()) {
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

func (u *Unchoker) TickUnchoke() {
	peers := u.candidatesUnchoke()
	u.sortPeers(peers)
	var unchoked int
	for _, pe := range peers {
		if unchoked < u.numUnchoked {
			u.unchokePeer(pe)
			unchoked++
		} else {
			u.chokePeer(pe)
		}
	}
}

func (u *Unchoker) TickOptimisticUnchoke() {
	peers := u.candidatesUnchokeOptimistic()
	var unchoked int
	for _, pe := range peers {
		if unchoked < u.numOptimisticUnchoked {
			u.optimisticUnchokePeer(pe)
			unchoked++
		} else {
			u.chokePeer(pe)
		}
	}
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
			panic("must not optimistic unchoke already unchoked peer")
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
