package session

import (
	"net"
	"time"

	"github.com/cenkalti/rain/internal/acceptor"
	"github.com/cenkalti/rain/internal/allocator"
	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/verifier"
)

func (t *torrent) start() {
	// Do not start if already started.
	if t.errC != nil {
		return
	}

	// Stop announcing Stopped event if in "Stopping" state.
	if t.stoppedEventAnnouncer != nil {
		t.stoppedEventAnnouncer.Close()
		t.stoppedEventAnnouncer = nil
	}

	t.log.Info("starting torrent")
	t.errC = make(chan error, 1)
	t.portC = make(chan int, 1)
	t.lastError = nil

	if t.info != nil {
		if t.pieces != nil {
			if t.bitfield != nil {
				t.startAcceptor()
				t.startAnnouncers()
				t.startPieceDownloaders()
				t.startUnchokeTimers()
			} else {
				t.startVerifier()
			}
		} else {
			t.startAllocator()
		}
	} else {
		t.startAcceptor()
		t.startAnnouncers()
		t.startInfoDownloaders()
	}

	t.startStatsWriter()
	t.startSpeedCounter()
}

func (t *torrent) startStatsWriter() {
	if t.statsWriteTicker != nil {
		return
	}
	t.statsWriteTicker = time.NewTicker(t.config.StatsWriteInterval)
	t.statsWriteTickerC = t.statsWriteTicker.C
}

func (t *torrent) startSpeedCounter() {
	if t.speedCounterTicker != nil {
		return
	}
	t.speedCounterTicker = time.NewTicker(5 * time.Second)
	t.speedCounterTickerC = t.speedCounterTicker.C
}

func (t *torrent) startVerifier() {
	if t.verifier != nil {
		panic("verifier exists")
	}
	t.verifier = verifier.New()
	go t.verifier.Run(t.pieces, t.verifierProgressC, t.verifierResultC)
}

func (t *torrent) startAllocator() {
	if t.allocator != nil {
		panic("allocator exists")
	}
	t.allocator = allocator.New()
	go t.allocator.Run(t.info, t.storage, t.allocatorProgressC, t.allocatorResultC)
}

func (t *torrent) startAnnouncers() {
	if len(t.announcers) > 0 {
		return
	}
	for _, tr := range t.trackers {
		an := announcer.NewPeriodicalAnnouncer(tr, t.config.TrackerNumWant, t.config.TrackerMinAnnounceInterval, t.announcerRequestC, t.completeC, t.addrsFromTrackers, t.log)
		t.announcers = append(t.announcers, an)
		go an.Run()
	}
	if t.dhtNode != nil && t.dhtAnnouncer == nil {
		t.dhtAnnouncer = announcer.NewDHTAnnouncer()
		go t.dhtAnnouncer.Run(t.dhtNode.Announce, t.config.DHTAnnounceInterval, t.config.DHTMinAnnounceInterval, t.log)
	}
}

func (t *torrent) startAcceptor() {
	if t.acceptor != nil {
		return
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: t.port})
	if err != nil {
		t.log.Warningf("cannot listen port %d: %s", t.port, err)
	} else {
		t.log.Notice("Listening peers on tcp://" + listener.Addr().String())
		t.port = listener.Addr().(*net.TCPAddr).Port
		t.portC <- t.port
		t.acceptor = acceptor.New(listener, t.incomingConnC, t.log)
		go t.acceptor.Run()
	}
}

func (t *torrent) startUnchokeTimers() {
	if t.unchokeTimer == nil {
		t.unchokeTimer = time.NewTicker(10 * time.Second)
		t.unchokeTimerC = t.unchokeTimer.C
	}
	if t.optimisticUnchokeTimer == nil {
		t.optimisticUnchokeTimer = time.NewTicker(30 * time.Second)
		t.optimisticUnchokeTimerC = t.optimisticUnchokeTimer.C
	}
}

func (t *torrent) startInfoDownloaders() {
	if t.info != nil {
		return
	}
	for len(t.infoDownloaders)-len(t.infoDownloadersSnubbed) < t.config.ParallelMetadataDownloads {
		id := t.nextInfoDownload()
		if id == nil {
			break
		}
		t.log.Debugln("downloading info from", id.Peer.String())
		t.infoDownloaders[id.Peer] = id
		id.RequestBlocks(t.config.RequestQueueLength)
		id.Peer.ResetSnubTimer()
	}
}

func (t *torrent) startPieceDownloaders() {
	if t.bitfield == nil {
		return
	}
	if t.pieces == nil {
		return
	}
	if t.completed {
		return
	}
	for len(t.pieceDownloaders)-len(t.pieceDownloadersChoked)-len(t.pieceDownloadersSnubbed) < t.config.ParallelPieceDownloads {
		pi, pe := t.piecePicker.Pick()
		if pi == nil || pe == nil {
			break
		}
		pd := piecedownloader.New(pi, pe, t.piecePool.Get().([]byte))
		// t.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
		if _, ok := t.pieceDownloaders[pd.Peer]; ok {
			panic("peer already has a piece downloader")
		}
		t.pieceDownloaders[pd.Peer] = pd
		pd.Peer.Downloading = true
		pd.RequestBlocks(t.config.RequestQueueLength)
		pd.Peer.ResetSnubTimer()
	}
}
