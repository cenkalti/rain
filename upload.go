package rain

import "io"

// requestSelector decides which request to serve.
func (t *transfer) requestSelector() {
	for {
		// var request peer.RequestMessage

		// t.peersM.RLock()
		// for peer := range t.peers {
		// 	select {
		// 	case request = <-peer.requests:
		// 		break
		// 	default:
		// 	}
		// }
		// t.peersM.RUnlock()

		// if request.peer == nil {
		// 	time.Sleep(time.Second) // TODO remove sleep
		// 	continue
		// }

		t.serveC <- <-t.requestC
	}
}

// pieceUploader uploads single piece to a peer.
func (t *transfer) pieceUploader() {
	for {
		select {
		case req := <-t.serveC:
			piece := t.pieces[req.Index]

			// TODO do not read whole piece
			b := make([]byte, piece.Length)
			_, err := io.ReadFull(piece.Reader(), b)
			if err != nil {
				t.log.Error(err)
				return
			}

			err = req.Peer.SendPiece(piece.Index, req.Begin, b[req.Begin:req.Begin+req.Length])
			if err != nil {
				t.log.Error(err)
				return
			}
		case <-t.cancelC:
			return
		}
	}
}
