package rain

// requestSelector decides which request to serve.
func (t *Torrent) requestSelector() {
	// TODO We respond to upload requests in FIFO order for now.
	for {
		t.serveC <- <-t.requestC
	}
}

// pieceUploader uploads single piece to a peer.
func (t *Torrent) pieceUploader() {
	b := make([]byte, maxAllowedBlockSize)
	for {
		select {
		case req := <-t.serveC:
			piece := t.pieces[req.Index]
			data := b[:req.Length]

			if _, err := piece.Files.ReadAt(data, int64(req.Begin)); err != nil {
				t.log.Error(err)
				break
			}
			if err := req.Peer.SendPiece(piece.Index, req.Begin, data); err != nil {
				t.log.Error(err)
				break // nolint: megacheck
			}
		case <-t.stopC:
			return
		}
	}
}
