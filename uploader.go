package rain

import (
	"bufio"
	"encoding/binary"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
)

type uploader struct {
	transfer *transfer
	requestC chan peerRequest
	cancelC  chan struct{}
	log      logger.Logger
}

func newUploader(t *transfer) *uploader {
	return &uploader{
		transfer: t,
		requestC: make(chan peerRequest),
		cancelC:  make(chan struct{}),
		log:      t.log,
	}
}

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
		var request peerRequest

		u.transfer.peersM.RLock()
		for peer := range u.transfer.peers {
			select {
			case request = <-peer.requests:
				break
			default:
			}
		}
		u.transfer.peersM.RUnlock()

		if request.peer == nil {
			time.Sleep(time.Second) // TODO remove sleep
			continue
		}

		select {
		case u.requestC <- request:
		case <-u.cancelC:
			return
		}
	}
}

// pieceUploader uploads single piece to a peer.
func (u *uploader) pieceUploader() {
	for {
		select {
		case req := <-u.requestC:
			peer := req.peer
			piece := req.piece

			if peer.amChoking {
				if err := peer.sendMessage(protocol.Unchoke); err != nil {
					peer.log.Error(err)
					return
				}
			}

			// TODO do not read whole piece
			b := make([]byte, piece.length)
			_, err := piece.files.Read(b)
			if err != nil {
				peer.log.Error(err)
				return
			}

			buf := bufio.NewWriterSize(peer.conn, int(13+req.length))
			msgLen := 9 + req.length
			if err = binary.Write(buf, binary.BigEndian, msgLen); err != nil {
				peer.log.Error(err)
				return
			}
			buf.WriteByte(byte(protocol.Piece))
			if err = binary.Write(buf, binary.BigEndian, piece.index); err != nil {
				peer.log.Error(err)
				return
			}
			if err = binary.Write(buf, binary.BigEndian, req.begin); err != nil {
				peer.log.Error(err)
				return
			}
			buf.Write(b[req.begin : req.begin+req.length])
			if err = buf.Flush(); err != nil {
				peer.log.Error(err)
				return
			}
		case <-u.cancelC:
			return
		}
	}
}
