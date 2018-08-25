package peerids

import "sync"

type PeerIDs struct {
	ids map[[20]byte]struct{}
	m   sync.Mutex
}

func New() *PeerIDs {
	return &PeerIDs{
		ids: make(map[[20]byte]struct{}),
	}
}

func (p *PeerIDs) Add(peerID [20]byte) bool {
	p.m.Lock()
	defer p.m.Unlock()
	_, ok := p.ids[peerID]
	if ok {
		return false
	}
	p.ids[peerID] = struct{}{}
	return true
}

func (p *PeerIDs) Remove(peerID [20]byte) {
	p.m.Lock()
	delete(p.ids, peerID)
	p.m.Unlock()
}
