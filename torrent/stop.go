package torrent

import (
	"time"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/rcrowley/go-metrics"
)

func (t *torrent) stop(err error) {
	s := t.status()
	if s == Stopping || s == Stopped {
		return
	}

	t.log.Info("stopping torrent")
	t.lastError = err
	if err != nil && err != errClosed {
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

	if t.resume != nil && t.bitfield != nil {
		t.writeBitfield(false)
	}

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
		t.verifier.Close()
		t.verifier = nil
	}

	t.log.Debugln("stopping outgoing handshakers")
	t.stopOutgoingHandshakers()

	t.log.Debugln("stopping incoming handshakers")
	t.stopIncomingHandshakers()

	t.log.Debugln("stopping stats writer")
	t.stopStatsWriter()

	t.stopSpeedCounter()

	t.log.Debugln("clearing piece cache")
	t.pieceCache.Clear()

	// Stop periodical announcers first.
	t.log.Debugln("stopping announcers")
	announcers := t.announcers // keep a reference to the list before nilling in order to start StopAnnouncer
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
	if t.stoppedEventAnnouncer != nil {
		panic("stopped event announcer exists")
	}
	t.stoppedEventAnnouncer = announcer.NewStopAnnouncer(trackers, t.announcerFields(), t.config.TrackerStopTimeout, t.announcersStoppedC, t.log)

	go t.stoppedEventAnnouncer.Run()

	t.addrList.Reset()

}

func (t *torrent) stopStatsWriter() {
	t.writeStats()
	t.seedDurationUpdatedAt = time.Time{}
	t.statsWriteTicker.Stop()
	t.statsWriteTicker = nil
	t.statsWriteTickerC = nil
}

func (t *torrent) stopSpeedCounter() {
	t.speedCounterTicker.Stop()
	t.speedCounterTicker = nil
	t.speedCounterTickerC = nil
	t.downloadSpeed = metrics.NewEWMA1()
	t.uploadSpeed = metrics.NewEWMA1()
}

func (t *torrent) stopOutgoingHandshakers() {
	for oh := range t.outgoingHandshakers {
		oh.Close()
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
}

func (t *torrent) stopIncomingHandshakers() {
	for ih := range t.incomingHandshakers {
		ih.Close()
	}
	t.incomingHandshakers = make(map[*incominghandshaker.IncomingHandshaker]struct{})
}

func (t *torrent) closeData() {
	for _, f := range t.files {
		err := f.Close()
		if err != nil {
			t.log.Error(err)
		}
	}
	t.files = nil
	t.pieces = nil
	t.piecePicker = nil
	t.bytesAllocated = 0
	t.checkedPieces = 0
}

func (t *torrent) stopPeriodicalAnnouncers() {
	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil
	if t.dhtAnnouncer != nil {
		t.dhtAnnouncer.Close()
		t.dhtAnnouncer = nil
	}
}

func (t *torrent) stopAcceptor() {
	if t.acceptor != nil {
		t.acceptor.Close()
	}
	t.acceptor = nil
}

func (t *torrent) stopPeers() {
	for p := range t.peers {
		t.closePeer(p)
	}
}

func (t *torrent) stopUnchokeTimers() {
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

func (t *torrent) stopInfoDownloaders() {
	for _, id := range t.infoDownloaders {
		t.closeInfoDownloader(id)
	}
}

func (t *torrent) stopPiecedownloaders() {
	for _, pd := range t.pieceDownloaders {
		t.closePieceDownloader(pd)
	}
}
