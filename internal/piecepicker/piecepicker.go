package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerset"
	"github.com/cenkalti/rain/internal/piece"
)

type PiecePicker struct {
	pieces               []myPiece
	piecesByAvailability []*myPiece
	piecesByStalled      []*myPiece
	maxDuplicateDownload int
	available            uint32
	endgame              bool
}

type myPiece struct {
	*piece.Piece
	Having    peerset.PeerSet
	Requested peerset.PeerSet
	Snubbed   peerset.PeerSet
	Choked    peerset.PeerSet
}

func (p *myPiece) RunningDownloads() int {
	return p.Requested.Len() - p.Snubbed.Len() - p.Choked.Len()
}

func (p *myPiece) StalledDownloads() int {
	return p.Snubbed.Len() + p.Choked.Len()
}

func New(pieces []piece.Piece, maxDuplicateDownload int) *PiecePicker {
	ps := make([]myPiece, len(pieces))
	for i := range pieces {
		ps[i] = myPiece{Piece: &pieces[i]}
	}
	sps := make([]*myPiece, len(ps))
	sps2 := make([]*myPiece, len(ps))
	for i := range sps {
		sps[i] = &ps[i]
		sps2[i] = &ps[i]
	}
	return &PiecePicker{
		pieces:               ps,
		piecesByAvailability: sps,
		piecesByStalled:      sps2,
		maxDuplicateDownload: maxDuplicateDownload,
	}
}

func (p *PiecePicker) Available() uint32 {
	return p.available
}

func (p *PiecePicker) RequestedPeers(i uint32) []*peer.Peer {
	return p.pieces[i].Requested.Peers
}

func (p *PiecePicker) HandleHave(pe *peer.Peer, i uint32) {
	pe.Bitfield.Set(i)
	p.addHavingPeer(i, pe)
}

func (p *PiecePicker) HandleAllowedFast(pe *peer.Peer, i uint32) {
	pe.AllowedFast.Add(p.pieces[i].Piece)
}

func (p *PiecePicker) HandleSnubbed(pe *peer.Peer, i uint32) {
	p.pieces[i].Snubbed.Add(pe)
}

func (p *PiecePicker) HandleChoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Choked.Add(pe)
}

func (p *PiecePicker) HandleUnchoke(pe *peer.Peer, i uint32) {
	p.pieces[i].Choked.Remove(pe)
}

func (p *PiecePicker) HandleCancelDownload(pe *peer.Peer, i uint32) {
	p.pieces[i].Requested.Remove(pe)
	p.pieces[i].Snubbed.Remove(pe)
}

func (p *PiecePicker) HandleDisconnect(pe *peer.Peer) {
	for i := range p.pieces {
		p.HandleCancelDownload(pe, uint32(i))
		p.removeHavingPeer(i, pe)
	}
}

func (p *PiecePicker) addHavingPeer(i uint32, pe *peer.Peer) {
	ok := p.pieces[i].Having.Add(pe)
	if ok && p.pieces[i].Having.Len() == 1 {
		p.available++
	}
}

func (p *PiecePicker) removeHavingPeer(i int, pe *peer.Peer) {
	ok := p.pieces[i].Having.Remove(pe)
	if ok && p.pieces[i].Having.Len() == 0 {
		p.available--
	}
}

func (p *PiecePicker) PickFor(pe *peer.Peer) *piece.Piece {
	pi := p.findPiece(pe)
	if pi == nil {
		return nil
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece
}

func (p *PiecePicker) findPiece(pe *peer.Peer) *myPiece {
	// Peer is allowed to download only one piece at a time
	if pe.Downloading {
		return nil
	}
	// Pick allowed fast piece
	pi := p.pickAllowedFast(pe)
	if pi != nil {
		return pi
	}
	// Must be unchoked to request a peer
	if pe.PeerChoking {
		return nil
	}
	// Short path for endgame mode.
	if p.endgame {
		return p.pickEndgame(pe)
	}
	// Pieck rarest piece
	pi = p.pickRarest(pe)
	if pi != nil {
		return pi
	}
	// Check if endgame mode is activated
	if p.endgame {
		return p.pickEndgame(pe)
	}
	// Re-request stalled downloads
	return p.pickStalled(pe)
}

func (p *PiecePicker) pickAllowedFast(pe *peer.Peer) *myPiece {
	for _, pi := range pe.AllowedFast.Pieces {
		mp := &p.pieces[pi.Index]
		if mp.Done || mp.Writing {
			continue
		}
		if mp.Requested.Len() == 0 && mp.Having.Has(pe) {
			return mp
		}
	}
	return nil
}

func (p *PiecePicker) pickRarest(pe *peer.Peer) *myPiece {
	// Sort by rarity
	sort.Slice(p.piecesByAvailability, func(i, j int) bool {
		return len(p.piecesByAvailability[i].Having.Peers) < len(p.piecesByAvailability[j].Having.Peers)
	})
	var picked *myPiece
	var hasUnrequested bool
	// Select unrequested piece
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing {
			continue
		}
		if mp.Requested.Len() == 0 && mp.Having.Has(pe) {
			picked = mp
			break
		}
		if mp.Requested.Len() == 0 {
			hasUnrequested = true
		}
	}
	if picked == nil && !hasUnrequested {
		p.endgame = true
	}
	return picked
}

func (p *PiecePicker) pickEndgame(pe *peer.Peer) *myPiece {
	// Sort by request count
	sort.Slice(p.piecesByAvailability, func(i, j int) bool {
		return p.piecesByAvailability[i].RunningDownloads() < p.piecesByAvailability[j].RunningDownloads()
	})
	// Select unrequested piece
	for _, mp := range p.piecesByAvailability {
		if mp.Done || mp.Writing {
			continue
		}
		if mp.Requested.Len() < p.maxDuplicateDownload && mp.Having.Has(pe) {
			return mp
		}
	}
	return nil
}

func (p *PiecePicker) pickStalled(pe *peer.Peer) *myPiece {
	// Sort by request count
	sort.Slice(p.piecesByStalled, func(i, j int) bool {
		return p.piecesByStalled[i].StalledDownloads() < p.piecesByStalled[j].StalledDownloads()
	})
	// Select unrequested piece
	for _, mp := range p.piecesByStalled {
		if mp.Done || mp.Writing {
			continue
		}
		if mp.RunningDownloads() > 0 {
			continue
		}
		if mp.Requested.Len() < p.maxDuplicateDownload && mp.Having.Has(pe) {
			return mp
		}
	}
	return nil
}
