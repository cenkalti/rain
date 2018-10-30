package torrent

import (
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

func (t *Torrent) stop(err error) {
	s := t.status()
	if s == Stopping || s == Stopped {
		return
	}

	t.log.Info("stopping torrent")
	t.lastError = err
	if err != nil {
		t.log.Error(err)
	}

	t.log.Debugln("stopping acceptor")
	t.stopAcceptor()

	t.log.Debugln("closing peer connections")
	t.stopPeers()

	t.log.Debugln("stopping piece downloaders")
	t.stopPiecedownloaders()

	t.log.Debugln("stopping info downloaders")
	t.stopInfoDownloaders()

	t.log.Debugln("stopping unchoke timers")
	t.stopUnchokeTimers()

	// Closing data is necessary to cancel ongoing IO operations on files.
	t.log.Debugln("closing open files")
	t.closeData()

	// Data must be closed before closing Allocator.
	t.log.Debugln("stopping allocator")
	if t.allocator != nil {
		t.allocator.Close()
		t.allocator = nil
	}

	// Data must be closed before closing Verifier.
	t.log.Debugln("stopping verifier")
	if t.verifier != nil {
		// TODO add close method to Verifier
		t.verifier.Stop()
		<-t.verifier.Done()
		t.verifier = nil
	}

	t.log.Debugln("closing outgoing handshakers")
	for oh := range t.outgoingHandshakers {
		oh.Close()
	}
	// TODO reset t.outgoingHandshakers struct

	t.log.Debugln("closing incoming handshakers")
	for ih := range t.incomingHandshakers {
		ih.Close()
	}
	// TODO reset t.incomingHandshakers struct

	// Stop periodical announcers first.
	t.log.Debugln("stopping announcers")
	announcers := t.announcers
	t.stopPeriodicalAnnouncers()

	// Then start another announcer to announce Stopped event to the trackers.
	// The torrent enters "Stopping" state.
	// This announcer times out in 5 seconds. After it's done the torrent is in "Stopped" status.
	trackers := make([]tracker.Tracker, 0, len(announcers))
	for _, an := range announcers {
		if an.HasAnnounced {
			trackers = append(trackers, an.Tracker)
		}
	}
	t.stoppedEventAnnouncer = announcer.NewStopAnnouncer(trackers, t.announcerFields(), t.config.TrackerStopTimeout, t.announcersStoppedC, t.log)

	go t.stoppedEventAnnouncer.Run()
}

func (t *Torrent) closeData() {
	if t.data != nil {
		t.data.Close()
		t.data = nil
	}
}

func (t *Torrent) stopPeriodicalAnnouncers() {
	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil
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
