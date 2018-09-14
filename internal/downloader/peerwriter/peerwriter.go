package peerwriter

import (
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/peer"
)

type PeerWriter struct {
	*peer.Peer
	requests  []Request
	RequestC  chan Request
	ChokeC    chan struct{}
	BitfieldC chan *bitfield.Bitfield
	writeC    chan Request
}

func New(p *peer.Peer) *PeerWriter {
	return &PeerWriter{
		Peer:      p,
		RequestC:  make(chan Request),
		ChokeC:    make(chan struct{}, 1),
		BitfieldC: make(chan *bitfield.Bitfield),
		writeC:    make(chan Request),
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
		case bf := <-p.BitfieldC:
			var err error
			if p.FastExtension && bf != nil && bf.All() {
				err = p.SendHaveAll()
			} else if p.FastExtension && bf != nil && bf.Count() == 0 {
				err = p.SendHaveNone()
			} else if bf != nil {
				err = p.SendBitfield(bf)
			}
			if err != nil {
				p.Logger().Errorln("cannot send bitfield", err)
			}
		case <-stopC:
			return
		}
	}
}
