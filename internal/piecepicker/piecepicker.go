package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
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
	Having      peerSet
	AllowedFast peerSet
	Requested   peerSet
	Snubbed     peerSet
	Choked      peerSet
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

func (p *PiecePicker) Pick() (*piece.Piece, *peer.Peer) {
	pi, pe := p.findPieceAndPeer(nil)
	if pi == nil || pe == nil {
		return nil, nil
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece, pe
}

func (p *PiecePicker) PickFor(cp *peer.Peer) *piece.Piece {
	pi, pe := p.findPieceAndPeer(cp)
	if pi == nil || pe == nil {
		return nil
	}
	if pe != cp {
		panic("invalid peer")
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece
}

func (p *PiecePicker) findPieceAndPeer(cp *peer.Peer) (*myPiece, *peer.Peer) {
	sort.Slice(p.sortedPieces, func(i, j int) bool {
		return len(p.sortedPieces[i].Having.Peers) < len(p.sortedPieces[j].Having.Peers)
	})
	pe, pi := p.selectAllowedFastPiece(cp, true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectUnchokedPeer(cp, true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectSnubbedPeer(cp, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectDuplicatePiece(cp)
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (p *PiecePicker) selectAllowedFastPiece(cp *peer.Peer, skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
	return p.selectPiece(cp, true, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectUnchokedPeer(cp *peer.Peer, skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
	return p.selectPiece(cp, false, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectSnubbedPeer(cp *peer.Peer, noDuplicate bool) (*myPiece, *peer.Peer) {
	pi, pe := p.selectAllowedFastPiece(cp, false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	pi, pe = p.selectUnchokedPeer(cp, false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	return nil, nil
}

func (p *PiecePicker) selectDuplicatePiece(cp *peer.Peer) (*myPiece, *peer.Peer) {
	pe, pi := p.selectAllowedFastPiece(cp, true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectUnchokedPeer(cp, true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectSnubbedPeer(cp, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (p *PiecePicker) selectPiece(pe *peer.Peer, preferAllowedFast, skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
	if pe != nil {
		if pe.Downloading {
			return nil, nil
		}
		if skipSnubbed && pe.Snubbed {
			return nil, nil
		}
		if !preferAllowedFast && pe.PeerChoking {
			return nil, nil
		}
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
		var l []*peer.Peer
		if pe != nil {
			l = []*peer.Peer{pe}
		} else {
			l = pi.Having.Peers
		}
		for _, pe := range l {
			if pe.Downloading {
				continue
			}
			if skipSnubbed && pe.Snubbed {
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
			return pi, pe
		}
	}
	return nil, nil
}
