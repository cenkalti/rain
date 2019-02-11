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
	Having      *peerList
	AllowedFast *peerList
	Requested   *peerList
	Snubbed     *peerList
}

func (p *myPiece) RunningDownloads() int {
	return p.Requested.Len() - p.Snubbed.Len()
}

func New(pieces []piece.Piece, endgameParallelDownloadsPerPiece int, l logger.Logger) *PiecePicker {
	ps := make([]myPiece, len(pieces))
	for i := range pieces {
		ps[i] = myPiece{
			Piece:       &pieces[i],
			Having:      new(peerList),
			AllowedFast: new(peerList),
			Requested:   new(peerList),
			Snubbed:     new(peerList),
		}
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
	pi, pe := p.findPieceAndPeer()
	if pi == nil || pe == nil {
		return nil, nil
	}
	pe.Snubbed = false
	pi.Requested.Add(pe)
	return pi.Piece, pe
}

func (p *PiecePicker) xPickFor(pe *peer.Peer) *piece.Piece {
	// TODO implement
	return nil
}

func (p *PiecePicker) findPieceAndPeer() (*myPiece, *peer.Peer) {
	sort.Slice(p.sortedPieces, func(i, j int) bool {
		return len(p.sortedPieces[i].Having.Peers) < len(p.sortedPieces[j].Having.Peers)
	})
	pe, pi := p.selectAllowedFastPiece(true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectUnchokedPeer(true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectSnubbedPeer(true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectDuplicatePiece()
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (p *PiecePicker) selectAllowedFastPiece(skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
	return p.selectPiece(true, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectUnchokedPeer(skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
	return p.selectPiece(false, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectSnubbedPeer(noDuplicate bool) (*myPiece, *peer.Peer) {
	pi, pe := p.selectAllowedFastPiece(false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	pi, pe = p.selectUnchokedPeer(false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	return nil, nil
}

func (p *PiecePicker) selectDuplicatePiece() (*myPiece, *peer.Peer) {
	pe, pi := p.selectAllowedFastPiece(true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectUnchokedPeer(true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectSnubbedPeer(false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (p *PiecePicker) selectPiece(preferAllowedFast, skipSnubbed, noDuplicate bool) (*myPiece, *peer.Peer) {
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
		for _, pe := range pi.Having.Peers {
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
