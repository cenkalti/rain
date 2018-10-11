package torrent

import (
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
)

func (t *Torrent) stop(err error) {
	if !t.running() {
		return
	}

	t.log.Info("stopping torrent")
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

	t.log.Debugln("stopping piece writers")
	for _, pw := range t.pieceWriters {
		pw.Close()
	}
	t.pieceWriters = nil

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
		p.Close()
	}
	t.peers = make(map[*peer.Peer]struct{})
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
		id.Close()
	}
	t.infoDownloaders = make(map[*peer.Peer]*infodownloader.InfoDownloader)
}

func (t *Torrent) stopPiecedownloaders() {
	for _, pd := range t.pieceDownloaders {
		pd.Close()
	}
	t.pieceDownloaders = make(map[*peer.Peer]*piecedownloader.PieceDownloader)
}
