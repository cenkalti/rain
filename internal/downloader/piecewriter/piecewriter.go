package piecewriter

import (
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/piece"
)

type PieceWriter struct {
	requests  chan Request
	responses chan Response
	log       logger.Logger
}

type Request struct {
	Piece *piece.Piece
	Data  []byte
}

type Response struct {
	Request Request
	Error   error
}

func New(requests chan Request, responses chan Response, l logger.Logger) *PieceWriter {
	return &PieceWriter{
		requests:  requests,
		responses: responses,
		log:       l,
	}
}

func (w *PieceWriter) Run(stopC chan struct{}) {
	for {
		select {
		case req := <-w.requests:
			w.log.Debugln("writing piece index:", req.Piece.Index, "len:", len(req.Data))
			resp := Response{Request: req}
			_, resp.Error = req.Piece.Data.Write(req.Data)
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
