package piecepicker

import (
	"sort"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/webseedsource"
)

// WebseedDownloadSpec contains information for downloading torrent data from webseed sources.
type WebseedDownloadSpec struct {
	Source *webseedsource.WebseedSource
	Begin  uint32
	End    uint32
}

// PickWebseed returns the next spec for downloading files from webseed sources.
func (p *PiecePicker) PickWebseed(src *webseedsource.WebseedSource) *WebseedDownloadSpec {
	begin, end := p.findRangeForWebseed()
	if begin == end {
		return nil
	}
	// Mark selected range as being downloaded so we won't select it again.
	for i := begin; i < end; i++ {
		if p.pieces[i].RequestedWebseed != nil {
			panic("already downloading from webseed url")
		}
		p.pieces[i].RequestedWebseed = src
	}
	return &WebseedDownloadSpec{
		Source: src,
		Begin:  begin,
		End:    end,
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

func (p *PiecePicker) findRangeForWebseed() (begin, end uint32) {
	gaps := p.findGaps()
	if len(gaps) == 0 {
		gap := p.webseedStealsFromAnotherWebseed()
		return gap.Begin, gap.End
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Len() > gaps[j].Len() })
	return gaps[0].Begin, gaps[0].End
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

func (p *PiecePicker) webseedStealsFromAnotherWebseed() (r Range) {
	downloading := p.getDownloadingSources()
	if len(downloading) == 0 {
		return
	}
	sort.Slice(downloading, func(i, j int) bool { return downloading[i].Remaining() > downloading[j].Remaining() })
	src := downloading[0]
	r.End = src.Downloader.End
	r.Begin = (src.Downloader.ReadCurrent() + src.Downloader.End + 1) / 2
	p.WebseedStopAt(src, r.Begin)
	return
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
	gaps := p.findGaps2(false)
	if len(gaps) == 0 {
		gaps = p.findGaps2(true)
	}
	return gaps
}

func (p *PiecePicker) findGaps2(duplicate bool) []Range {
	a := make([]Range, 0, len(p.pieces)/2)
	var inGap bool
	var begin uint32
	for _, pi := range p.pieces {
		if !inGap {
			if pi.AvailableForWebseed(duplicate) {
				begin = pi.Index
				inGap = true
			} else {
				continue
			}
		} else {
			if pi.AvailableForWebseed(duplicate) {
				continue
			} else {
				a = append(a, Range{Begin: begin, End: pi.Index})
				inGap = false
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
		for i := gap.End - 1; i >= gap.Begin; i-- {
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
