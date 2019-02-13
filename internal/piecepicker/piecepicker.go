package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerset"
	"github.com/cenkalti/rain/internal/piece"
)

type PiecePicker struct {
	pieces                           []myPiece
	sortedPieces                     []*myPiece
	endgameParallelDownloadsPerPiece int
	available                        uint32
	log                              logger.Logger
}

type myPiece struct {
	*piece.Piece
	Having      peerset.PeerSet
	AllowedFast peerset.PeerSet
	Requested   peerset.PeerSet
	Snubbed     peerset.PeerSet
	Choked      peerset.PeerSet
}

func (p *myPiece) RunningDownloads() int {
	return p.Requested.Len() - p.Snubbed.Len()
}

func New(pieces []piece.Piece, endgameParallelDownloadsPerPiece int, l logger.Logger) *PiecePicker {
	ps := make([]myPiece, len(pieces))
	for i := range pieces {
		ps[i] = myPiece{Piece: &pieces[i]}
	}
	sps := make([]*myPiece, len(ps))
	for i := range sps {
		sps[i] = &ps[i]
	}
	return &PiecePicker{
		pieces:                           ps,
		sortedPieces:                     sps,
		endgameParallelDownloadsPerPiece: endgameParallelDownloadsPerPiece,
		log:                              l,
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
	p.pieces[i].AllowedFast.Add(pe)
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
		p.pieces[i].AllowedFast.Remove(pe)
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
	if pe.Downloading {
		return nil
	}
	pi := p.findPiece(pe)
	if pi == nil {
		return nil
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece
}

func (p *PiecePicker) findPiece(pe *peer.Peer) *myPiece {
	sort.Slice(p.sortedPieces, func(i, j int) bool {
		return len(p.sortedPieces[i].Having.Peers) < len(p.sortedPieces[j].Having.Peers)
	})
	pi := p.selectAllowedFastPiece(pe, true, true)
	if pi != nil {
		return pi
	}
	pi = p.selectUnchokedPeer(pe, true, true)
	if pi != nil {
		return pi
	}
	pi = p.selectSnubbedPeer(pe, true)
	if pi != nil {
		return pi
	}
	pi = p.selectDuplicatePiece(pe)
	if pi != nil {
		return pi
	}
	return nil
}

func (p *PiecePicker) selectAllowedFastPiece(pe *peer.Peer, skipSnubbed, noDuplicate bool) *myPiece {
	return p.selectPiece(pe, true, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectUnchokedPeer(pe *peer.Peer, skipSnubbed, noDuplicate bool) *myPiece {
	return p.selectPiece(pe, false, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectSnubbedPeer(pe *peer.Peer, noDuplicate bool) *myPiece {
	pi := p.selectAllowedFastPiece(pe, false, noDuplicate)
	if pi != nil {
		return pi
	}
	pi = p.selectUnchokedPeer(pe, false, noDuplicate)
	if pi != nil {
		return pi
	}
	return nil
}

func (p *PiecePicker) selectDuplicatePiece(pe *peer.Peer) *myPiece {
	pi := p.selectAllowedFastPiece(pe, true, false)
	if pi != nil {
		return pi
	}
	pi = p.selectUnchokedPeer(pe, true, false)
	if pi != nil {
		return pi
	}
	pi = p.selectSnubbedPeer(pe, false)
	if pi != nil {
		return pi
	}
	return nil
}

func (p *PiecePicker) selectPiece(pe *peer.Peer, preferAllowedFast, skipSnubbed, noDuplicate bool) *myPiece {
	if skipSnubbed && pe.Snubbed {
		return nil
	}
	if !preferAllowedFast && pe.PeerChoking {
		return nil
	}
	for _, pi := range p.sortedPieces {
		if pi.Done {
			continue
		}
		if pi.Writing {
			continue
		}
		if noDuplicate && pi.Requested.Len() > 0 {
			continue
		} else if pi.RunningDownloads() >= p.endgameParallelDownloadsPerPiece {
			continue
		}
		if preferAllowedFast {
			if !pi.AllowedFast.Has(pe) {
				continue
			}
		} else {
			if pe.PeerChoking {
				continue
			}
		}
		return pi
	}
	return nil
}
