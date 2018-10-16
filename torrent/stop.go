package torrent

func (t *Torrent) stop(err error) {
	if !t.running() {
		return
	}

	t.log.Info("stopping torrent")
	if err != nil && err != errClosed {
		t.log.Error(err)
	}
	t.errC <- err
	t.errC = nil

	t.log.Debugln("stopping acceptor")
	t.stopAcceptor()

	t.log.Debugln("closing peer connections")
	t.stopPeers()

	t.log.Debugln("stopping piece downloaders")
	t.stopPiecedownloaders()

	t.log.Debugln("stopping info downloaders")
	t.stopInfoDownloaders()

	t.log.Debugln("stopping announcers")
	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil

	t.log.Debugln("stopping unchoke timers")
	t.stopUnchokeTimers()

	t.log.Debugln("stopping allocator")
	if t.allocator != nil {
		t.allocator.Close()
		t.allocator = nil
	}

	t.log.Debugln("stopping verifier")
	if t.verifier != nil {
		t.verifier.Stop()
		<-t.verifier.Done()
		t.verifier = nil
	}

	t.log.Debugln("closing outgoing handshakers")
	for _, oh := range t.outgoingHandshakers {
		oh.Close()
	}

	t.log.Debugln("closing incoming handshakers")
	for _, ih := range t.incomingHandshakers {
		ih.Close()
	}
}

func (t *Torrent) stopAcceptor() {
	if t.acceptor != nil {
		t.acceptor.Close()
	}
	t.acceptor = nil
}

func (t *Torrent) stopPeers() {
	for p := range t.peers {
		t.closePeer(p)
	}
}

func (t *Torrent) stopUnchokeTimers() {
	if t.unchokeTimer != nil {
		t.unchokeTimer.Stop()
		t.unchokeTimer = nil
		t.unchokeTimerC = nil
	}
	if t.optimisticUnchokeTimer != nil {
		t.optimisticUnchokeTimer.Stop()
		t.optimisticUnchokeTimer = nil
		t.optimisticUnchokeTimerC = nil
	}
}

func (t *Torrent) stopInfoDownloaders() {
	for _, id := range t.infoDownloaders {
		t.closeInfoDownloader(id)
	}
}

func (t *Torrent) stopPiecedownloaders() {
	for _, pd := range t.pieceDownloaders {
		t.closePieceDownloader(pd)
	}
}
