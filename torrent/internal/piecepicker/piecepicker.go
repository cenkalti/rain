package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/peer"
)

type PiecePicker struct {
	pieces                           []Piece
	sortedPieces                     []*Piece
	endgameParallelDownloadsPerPiece int
	available                        uint32
	log                              logger.Logger
}

type Piece struct {
	Index            uint32
	HavingPeers      map[*peer.Peer]struct{}
	AllowedFastPeers map[*peer.Peer]struct{}
	RequestedPeers   map[*peer.Peer]struct{}
	Writing          bool
	Done             bool
}

func (p *Piece) RunningDownloads() int {
	n := len(p.RequestedPeers)
	for pe := range p.RequestedPeers {
		if pe.Snubbed {
			n--
		}
	}
	return n
}

func New(numPieces uint32, endgameParallelDownloadsPerPiece int, l logger.Logger) *PiecePicker {
	ps := make([]Piece, numPieces)
	for i := range ps {
		ps[i] = Piece{
			Index:            uint32(i),
			HavingPeers:      make(map[*peer.Peer]struct{}),
			AllowedFastPeers: make(map[*peer.Peer]struct{}),
			RequestedPeers:   make(map[*peer.Peer]struct{}),
		}
	}
	sps := make([]*Piece, len(ps))
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

func (p *PiecePicker) RequestedPeers(i uint32) map[*peer.Peer]struct{} {
	return p.pieces[i].RequestedPeers
}

func (p *PiecePicker) DoesHave(pe *peer.Peer, i uint32) bool {
	_, ok := p.pieces[i].HavingPeers[pe]
	return ok
}

func (p *PiecePicker) HandleHave(pe *peer.Peer, i uint32) {
	p.pieces[i].HavingPeers[pe] = struct{}{}
	if len(p.pieces[i].HavingPeers) == 1 {
		p.available++
	}
}

func (p *PiecePicker) HandleAllowedFast(pe *peer.Peer, i uint32) {
	p.pieces[i].AllowedFastPeers[pe] = struct{}{}
}

func (p *PiecePicker) HandleCancelDownload(pe *peer.Peer, i uint32) {
	delete(p.pieces[i].RequestedPeers, pe)
}

func (p *PiecePicker) HandleDisconnect(pe *peer.Peer) {
	for i := range p.pieces {
		delete(p.pieces[i].HavingPeers, pe)
		delete(p.pieces[i].AllowedFastPeers, pe)
		delete(p.pieces[i].RequestedPeers, pe)
		if len(p.pieces[i].HavingPeers) == 0 {
			p.available--
		}
	}
}

func (p *PiecePicker) HandleWriting(i uint32) {
	p.pieces[i].Writing = true
}

func (p *PiecePicker) HandleWriteSuccess(i uint32) {
	p.pieces[i].Writing = false
	p.pieces[i].Done = true
}

func (p *PiecePicker) HandleWriteError(i uint32) {
	p.pieces[i].Writing = false
}

func (p *PiecePicker) Pick() (uint32, *peer.Peer) {
	pi, pe := p.findPieceAndPeer()
	if pi == nil || pe == nil {
		return 0, nil
	}
	pe.Snubbed = false
	pi.RequestedPeers[pe] = struct{}{}
	return pi.Index, pe
}

func (p *PiecePicker) findPieceAndPeer() (*Piece, *peer.Peer) {
	pe, pi := p.select4RandomPiece()
	if pe != nil && pi != nil {
		return pe, pi
	}
	sort.Slice(p.sortedPieces, func(i, j int) bool { return len(p.sortedPieces[i].HavingPeers) < len(p.sortedPieces[j].HavingPeers) })
	pe, pi = p.selectAllowedFastPiece(true, true)
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

func (p *PiecePicker) select4RandomPiece() (*Piece, *peer.Peer) {
	// TODO request first 4 pieces randomly
	return nil, nil
}

func (p *PiecePicker) selectAllowedFastPiece(skipSnubbed, noDuplicate bool) (*Piece, *peer.Peer) {
	return p.selectPiece(true, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectUnchokedPeer(skipSnubbed, noDuplicate bool) (*Piece, *peer.Peer) {
	return p.selectPiece(false, skipSnubbed, noDuplicate)
}

func (p *PiecePicker) selectSnubbedPeer(noDuplicate bool) (*Piece, *peer.Peer) {
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

func (p *PiecePicker) selectDuplicatePiece() (*Piece, *peer.Peer) {
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

func (p *PiecePicker) selectPiece(preferAllowedFast, skipSnubbed, noDuplicate bool) (*Piece, *peer.Peer) {
	for _, pi := range p.sortedPieces {
		if pi.Done {
			continue
		}
		if pi.Writing {
			continue
		}
		if noDuplicate && len(pi.RequestedPeers) > 0 {
			continue
		} else if pi.RunningDownloads() >= p.endgameParallelDownloadsPerPiece {
			continue
		}
		for pe := range pi.HavingPeers {
			if pe.Downloading {
				continue
			}
			if skipSnubbed && pe.Snubbed {
				continue
			}
			if preferAllowedFast {
				if _, ok := pi.AllowedFastPeers[pe]; !ok {
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
