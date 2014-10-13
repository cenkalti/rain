package rain

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

			// TODO Copy directly to conn
			b := make([]byte, req.Length)
			_, err := piece.files.ReadAt(b, int64(req.Begin))
			if err != nil {
				t.log.Error(err)
				return
			}

			err = req.Peer.SendPiece(piece.Index, req.Begin, b)
			if err != nil {
				t.log.Error(err)
				return
			}
		case <-t.stopC:
			return
		}
	}
}
