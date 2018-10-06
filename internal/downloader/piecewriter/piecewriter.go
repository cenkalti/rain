package piecewriter

import (
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/pieceio"
)

type PieceWriter struct {
	requests  chan Request
	responses chan Response
	closeC    chan struct{}
	closedC   chan struct{}
	log       logger.Logger
}

type Request struct {
	Piece *pieceio.Piece
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
		closeC:    make(chan struct{}),
		closedC:   make(chan struct{}),
		log:       l,
	}
}

func (w *PieceWriter) Close() {
	close(w.closeC)
	<-w.closedC
}

func (w *PieceWriter) Run() {
	defer close(w.closedC)
	for {
		select {
		case req := <-w.requests:
			w.log.Debugln("writing piece index:", req.Piece.Index, "len:", len(req.Data))
			resp := Response{Request: req}
			_, resp.Error = req.Piece.Data.Write(req.Data)
			select {
			case w.responses <- resp:
			case <-w.closeC:
				return
			}
		case <-w.closeC:
			return
		}
	}
}
