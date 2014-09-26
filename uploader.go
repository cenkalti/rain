package rain

import (
	"io"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/peer"
)

type uploader struct {
	transfer *transfer
	requestC chan *peer.Request
	serveC   chan *peer.Request
	cancelC  chan struct{}
	log      logger.Logger
}

func newUploader(t *transfer) *uploader {
	return &uploader{
		transfer: t,
		requestC: make(chan *peer.Request),
		serveC:   make(chan *peer.Request),
		cancelC:  make(chan struct{}),
		log:      t.log,
	}
}

func (u *uploader) RequestC() chan *peer.Request { return u.requestC }

func (u *uploader) Run() {
	go u.requestSelector()
	for i := 0; i < uploadSlots; i++ {
		go u.pieceUploader()
	}
	<-u.cancelC
}

// requestSelector decides which request to serve.
func (u *uploader) requestSelector() {
	for {
		// var request peer.Request

		// u.transfer.peersM.RLock()
		// for peer := range u.transfer.peers {
		// 	select {
		// 	case request = <-peer.requests:
		// 		break
		// 	default:
		// 	}
		// }
		// u.transfer.peersM.RUnlock()

		// if request.peer == nil {
		// 	time.Sleep(time.Second) // TODO remove sleep
		// 	continue
		// }

		u.serveC <- <-u.requestC
	}
}

// pieceUploader uploads single piece to a peer.
func (u *uploader) pieceUploader() {
	for {
		select {
		case req := <-u.serveC:
			piece := u.transfer.pieces[req.Index]

			// TODO do not read whole piece
			b := make([]byte, piece.Length())
			_, err := io.ReadFull(piece.Reader(), b)
			if err != nil {
				u.transfer.log.Error(err)
				return
			}

			err = req.Peer.SendPiece(piece.Index(), req.Begin, b[req.Begin:req.Begin+req.Length])
			if err != nil {
				u.transfer.log.Error(err)
				return
			}
		case <-u.cancelC:
			return
		}
	}
}
