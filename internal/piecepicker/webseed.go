package piecepicker

import (
	"math/rand"
	"sort"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/webseedsource"
)

// WebseedDownloadSpec contains information for downloading torrent data from webseed sources.
type WebseedDownloadSpec struct {
	Source *webseedsource.WebseedSource
	Begin  uint32 // piece index
	End    uint32 // piece index
}

// PickWebseed returns the next spec for downloading files from webseed sources.
func (p *PiecePicker) PickWebseed(src *webseedsource.WebseedSource) *WebseedDownloadSpec {
	r := p.findPieceRangeForWebseed()
	if r == nil {
		return nil
	}
	// Mark selected range as being downloaded so we won't select it again.
	for i := r.Begin; i < r.End; i++ {
		if p.pieces[i].RequestedWebseed != nil {
			panic("already downloading from webseed url")
		}
		p.pieces[i].RequestedWebseed = src
	}
	return &WebseedDownloadSpec{
		Source: src,
		Begin:  r.Begin,
		End:    r.End,
	}
}

func (p *PiecePicker) downloadingWebseed() bool {
	for _, src := range p.webseedSources {
		if src.Downloading() {
			return true
		}
	}
	return false
}

func (p *PiecePicker) findPieceRangeForWebseed() *Range {
	gaps := p.findGaps()
	if len(gaps) == 0 {
		return p.webseedStealsFromAnotherWebseed()
	}
	gap := selectRandomLargestGap(gaps)
	return &gap
}

func selectRandomLargestGap(gaps []Range) Range {
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Len() > gaps[j].Len() })
	length := gaps[0].Len()
	for i := range gaps {
		if gaps[i].Len() != length {
			return gaps[rand.Intn(i)]
		}
	}
	return gaps[rand.Intn(len(gaps))]
}

func (p *PiecePicker) getDownloadingSources() []*webseedsource.WebseedSource {
	ret := make([]*webseedsource.WebseedSource, 0, len(p.webseedSources))
	for _, src := range p.webseedSources {
		if src.Downloading() {
			ret = append(ret, src)
		}
	}
	return ret
}

func (p *PiecePicker) webseedStealsFromAnotherWebseed() *Range {
	downloading := p.getDownloadingSources()
	if len(downloading) == 0 {
		return nil
	}
	sort.Slice(downloading, func(i, j int) bool { return downloading[i].Remaining() > downloading[j].Remaining() })
	src := downloading[0]
	r := &Range{
		Begin: (src.Downloader.ReadCurrent() + src.Downloader.End + 1) / 2,
		End:   src.Downloader.End,
	}
	if r.Begin >= r.End {
		return nil
	}
	p.WebseedStopAt(src, r.Begin)
	return r
}

func (p *PiecePicker) peerStealsFromWebseed(pe *peer.Peer) *myPiece {
	downloading := p.getDownloadingSources()
	for _, src := range downloading {
		if src.Remaining() == 0 {
			continue
		}
		for i := src.Downloader.End - 1; i > src.Downloader.ReadCurrent(); i-- {
			pi := &p.pieces[i]
			if pi.Done || pi.Writing {
				continue
			}
			if !pi.Having.Has(pe) {
				continue
			}
			if pi.Requested.Len() > 0 {
				continue
			}
			p.WebseedStopAt(src, i)
			return pi
		}
	}
	return nil
}

func (p *PiecePicker) findGaps() []Range {
	a := make([]Range, 0, len(p.pieces)/2)
	var inGap bool // See BEP19 for definition of "gap".
	var begin uint32
	for _, pi := range p.pieces {
		if !inGap {
			if pi.AvailableForWebseed() {
				begin = pi.Index
				inGap = true
			}
		} else {
			r := Range{Begin: begin, End: pi.Index}
			if !pi.AvailableForWebseed() {
				a = append(a, r)
				inGap = false
			} else if r.Len() == p.maxWebseedPieces {
				a = append(a, r)
				begin = pi.Index
			}
		}
	}
	if inGap {
		a = append(a, Range{Begin: begin, End: uint32(len(p.pieces))})
	}
	return a
}

func (p *PiecePicker) pickLastPieceOfSmallestGap(pe *peer.Peer) *myPiece {
	gaps := p.findGaps()
	if len(gaps) == 0 {
		return nil
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Len() < gaps[j].Len() })
	for _, gap := range gaps {
		// Convert index to int because it goes below zero in loop.
		for i := int(gap.End - 1); i >= int(gap.Begin); i-- {
			mp := &p.pieces[i]
			if !mp.Having.Has(pe) {
				continue
			}
			if pe.PeerChoking && !pe.ReceivedAllowedFast.Has(mp.Piece) {
				continue
			}
			return mp
		}
	}
	return nil
}

// Range is a piece index range.
// Begin is inclusive, End is exclusive.
type Range struct {
	Begin, End uint32
}

// Len returns the number of pieces in the range.
func (r Range) Len() int {
	return int(r.End) - int(r.Begin)
}
