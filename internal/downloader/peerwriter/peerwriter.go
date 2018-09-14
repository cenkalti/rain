package peerwriter

import (
	"github.com/cenkalti/rain/internal/peer"
)

type PeerWriter struct {
	*peer.Peer
	requests []Request
	RequestC chan Request
	ChokeC   chan struct{}
	writeC   chan Request
}

func New(p *peer.Peer) *PeerWriter {
	return &PeerWriter{
		Peer:     p,
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
	buf := make([]byte, peer.MaxAllowedBlockSize)
	for {
		select {
		case req := <-p.writeC:
			b := buf[:req.Request.Length]
			err := req.Piece.Data.ReadAt(b, int64(req.Request.Begin))
			if err != nil {
				p.Logger().Errorln("cannot read piece data:", err)
				p.Peer.Close()
				return
			}
			req.Request.Peer.SendPiece(req.Piece.Index, req.Request.Begin, b)
		case <-p.ChokeC:
			_ = p.Peer.SendChoke()
			// TODO ignore allowed fast set
			for _, req := range p.requests {
				_ = p.Peer.SendReject(req.Piece.Index, req.Request.Begin, req.Request.Length)
			}
			p.requests = nil
		case <-stopC:
			return
		}
	}
}
