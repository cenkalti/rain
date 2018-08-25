package piecewriter

import (
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

type PieceWriter struct {
	messages chan Message
	data     *torrentdata.Data
	log      logger.Logger
}

type Message struct {
	Piece *piece.Piece
	Data  []byte
}

func New(data *torrentdata.Data, messages chan Message, l logger.Logger) *PieceWriter {
	return &PieceWriter{
		data:     data,
		messages: messages,
		log:      l,
	}
}

func (w *PieceWriter) Run(stopC chan struct{}) {
	for {
		select {
		case msg := <-w.messages:
			w.log.Debugln("writing piece index:", msg.Piece.Index, "len:", len(msg.Data))
			err := w.data.WritePiece(msg.Piece.Index, msg.Data)
			if err != nil {
				w.log.Error(err)
			}
		case <-stopC:
			return
		}
	}
}
