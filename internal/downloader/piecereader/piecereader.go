package piecereader

import (
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceReader struct {
	requests  chan Request
	responses chan Response
	log       logger.Logger
}

type Request struct {
	Peer   *peer.Peer
	Piece  *piece.Piece
	Begin  uint32
	Length uint32
}

type Response struct {
	Peer  *peer.Peer
	Index uint32
	Begin uint32
	Data  []byte
	Error error
}

func New(requests chan Request, responses chan Response, l logger.Logger) *PieceReader {
	return &PieceReader{
		requests:  requests,
		responses: responses,
		log:       l,
	}
}

func (w *PieceReader) Run(stopC chan struct{}) {
	for {
		select {
		case req := <-w.requests:
			w.log.Debugln("reading piece index:", req.Piece.Index, "begin:", req.Begin)
			// TODO reduce allocation
			buf := make([]byte, peer.MaxAllowedBlockSize)
			b := buf[:req.Length]
			err := req.Piece.Data.ReadAt(b, int64(req.Begin))
			if err != nil {
				w.log.Errorln("cannot read piece data:", err)
				break
			}
			resp := Response{
				Peer:  req.Peer,
				Index: req.Piece.Index,
				Begin: req.Begin,
				Data:  b,
				Error: err,
			}
			select {
			case w.responses <- resp:
			case <-stopC:
				return
			}
		case <-stopC:
			return
		}
	}
}
