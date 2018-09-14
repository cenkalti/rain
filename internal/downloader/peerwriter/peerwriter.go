package peerwriter

import (
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/peer"
)

type PeerWriter struct {
	*peer.Peer
	bitfield *bitfield.Bitfield
	requests []Request
	RequestC chan Request
	ChokeC   chan struct{}
	writeC   chan Request
}

func New(p *peer.Peer, bf *bitfield.Bitfield) *PeerWriter {
	return &PeerWriter{
		Peer:     p,
		bitfield: bf,
		RequestC: make(chan Request),
		ChokeC:   make(chan struct{}, 1),
		writeC:   make(chan Request),
	}
}

func (p *PeerWriter) Run(stopC chan struct{}) {
	go p.writer(stopC)
	for {
		if len(p.requests) == 0 {
			select {
			case req := <-p.RequestC:
				p.requests = append(p.requests, req)
				continue
			case <-stopC:
				close(p.writeC)
				return
			}
		}
		req := p.requests[0]
		select {
		case req = <-p.RequestC:
			p.requests = append(p.requests, req)
		case p.writeC <- req:
			p.requests = p.requests[1:]
		case <-stopC:
			close(p.writeC)
			return
		}
	}
}

func (p *PeerWriter) writer(stopC chan struct{}) {
	var err error
	if p.FastExtension && p.bitfield != nil && p.bitfield.All() {
		err = p.SendHaveAll()
	} else if p.FastExtension && p.bitfield != nil && p.bitfield.Count() == 0 {
		err = p.SendHaveNone()
	} else if p.bitfield != nil {
		err = p.SendBitfield(p.bitfield)
	}
	if err != nil {
		p.Logger().Errorln("cannot send bitfield", err)
	}

	buf := make([]byte, peer.MaxAllowedBlockSize)
	for {
		select {
		case req := <-p.writeC:
			b := buf[:req.Request.Length]
			err := req.Piece.Data.ReadAt(b, int64(req.Request.Begin))
			if err != nil {
				p.Logger().Errorln("cannot read piece data:", err)
				break
			}
			err = req.Request.Peer.SendPiece(req.Piece.Index, req.Request.Begin, b)
			if err != nil {
				p.Logger().Errorln("cannot send piece:", err)
				break
			}
		case <-p.ChokeC:
			err := p.Peer.SendChoke()
			if err != nil {
				p.Logger().Errorln("cannot send choke:", err)
				break
			}
			// TODO ignore allowed fast set
			for _, req := range p.requests {
				err = p.Peer.SendReject(req.Piece.Index, req.Request.Begin, req.Request.Length)
				if err != nil {
					p.Logger().Errorln("cannot send reject:", err)
				}
			}
			p.requests = nil
		case <-stopC:
			return
		}
	}
}
