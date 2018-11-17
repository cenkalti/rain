package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/peer"
)

type PiecePicker struct {
	pieces                           []piece
	sortedPieces                     []*piece
	endgameParallelDownloadsPerPiece int
	available                        uint32
	log                              logger.Logger
}

type piece struct {
	Index            uint32
	HavingPeers      map[*peer.Peer]struct{}
	AllowedFastPeers map[*peer.Peer]struct{}
	RequestedPeers   map[*peer.Peer]struct{}
	SnubbedPeers     map[*peer.Peer]struct{}
	Writing          bool
	Done             bool
}

func (p *piece) RunningDownloads() int {
	return len(p.RequestedPeers) - len(p.SnubbedPeers)
}

func New(numPieces uint32, endgameParallelDownloadsPerPiece int, l logger.Logger) *PiecePicker {
	ps := make([]piece, numPieces)
	for i := range ps {
		ps[i] = piece{
			Index:            uint32(i),
			HavingPeers:      make(map[*peer.Peer]struct{}),
			AllowedFastPeers: make(map[*peer.Peer]struct{}),
			RequestedPeers:   make(map[*peer.Peer]struct{}),
			SnubbedPeers:     make(map[*peer.Peer]struct{}),
		}
	}
	sps := make([]*piece, len(ps))
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

func (p *PiecePicker) HandleSnubbed(pe *peer.Peer, i uint32) {
	p.pieces[i].SnubbedPeers[pe] = struct{}{}
}

func (p *PiecePicker) HandleCancelDownload(pe *peer.Peer, i uint32) {
	delete(p.pieces[i].RequestedPeers, pe)
	delete(p.pieces[i].SnubbedPeers, pe)
}

func (p *PiecePicker) HandleDisconnect(pe *peer.Peer) {
	for i := range p.pieces {
		p.HandleCancelDownload(pe, uint32(i))
		delete(p.pieces[i].AllowedFastPeers, pe)
		delete(p.pieces[i].HavingPeers, pe)
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

func (p *PiecePicker) findPieceAndPeer() (*piece, *peer.Peer) {
	pe, pi := p.select4RandomPiece()
	if pe != nil && pi != nil {
		return pe, pi
	}
	sort.Slice(p.sortedPieces, func(i, j int) bool { return len(p.sortedPieces[i].HavingPeers) < len(p.sortedPieces[j].HavingPeers) })
	pe, pi = p.selectPiece(true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = p.selectPiece(false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (p *PiecePicker) select4RandomPiece() (*piece, *peer.Peer) {
	// TODO request first 4 pieces randomly
	return nil, nil
}

func (p *PiecePicker) selectPiece(noDuplicate bool) (*piece, *peer.Peer) {
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
			if !pe.PeerChoking {
				return pi, pe
			}
			if _, ok := pi.AllowedFastPeers[pe]; !ok {
				return pi, pe
			}
		}
	}
	return nil, nil
}
