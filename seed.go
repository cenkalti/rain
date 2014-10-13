package rain

import "io"

// requestSelector decides which request to serve.
func (t *Transfer) requestSelector() {
	// TODO We respond to upload requests in FIFO order for now.
	for {
		t.serveC <- <-t.requestC
	}
}

// pieceUploader uploads single piece to a peer.
func (t *Transfer) pieceUploader() {
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
		case <-t.stopC:
			return
		}
	}
}
