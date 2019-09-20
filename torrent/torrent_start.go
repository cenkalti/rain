package torrent

import (
	"net"

	"github.com/cenkalti/rain/internal/acceptor"
	"github.com/cenkalti/rain/internal/allocator"
	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/piecepicker"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/internal/urldownloader"
	"github.com/cenkalti/rain/internal/verifier"
	"github.com/cenkalti/rain/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
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
	t.downloadSpeed = metrics.NewMeter()
	t.uploadSpeed = metrics.NewMeter()

	if t.info != nil {
		if t.pieces != nil {
			if t.bitfield != nil {
				t.addFixedPeers()
				t.startAcceptor()
				t.startAnnouncers()
				t.startPieceDownloaders()
			} else {
				t.startVerifier()
			}
		} else {
			t.startAllocator()
		}
	} else {
		t.addFixedPeers()
		t.startAcceptor()
		t.startAnnouncers()
		t.startInfoDownloaders()
	}
}

func (t *torrent) startVerifier() {
	if t.verifier != nil {
		panic("verifier exists")
	}
	if len(t.pieces) == 0 {
		panic("zero length pieces")
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

func (t *torrent) addFixedPeers() {
	for _, pe := range t.fixedPeers {
		_ = t.addPeerString(pe)
	}
}

func (t *torrent) startAnnouncers() {
	if len(t.announcers) == 0 {
		for _, tr := range t.trackers {
			t.startNewAnnouncer(tr)
		}
	}
	if t.dhtAnnouncer == nil && t.session.config.DHTEnabled && (t.info == nil || !t.info.Private) {
		t.dhtAnnouncer = announcer.NewDHTAnnouncer()
		go t.dhtAnnouncer.Run(t.announceDHT, t.session.config.DHTAnnounceInterval, t.session.config.DHTMinAnnounceInterval, t.log)
	}
}

func (t *torrent) startNewAnnouncer(tr tracker.Tracker) {
	an := announcer.NewPeriodicalAnnouncer(
		tr,
		t.session.config.TrackerNumWant,
		t.session.config.TrackerMinAnnounceInterval,
		t.announcerFields,
		t.completeC,
		t.addrsFromTrackers,
		t.log,
	)
	t.announcers = append(t.announcers, an)
	go an.Run()
}

func (t *torrent) startAcceptor() {
	if t.acceptor != nil {
		return
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: t.port})
	if err != nil {
		t.log.Warningf("cannot listen port %d: %s", t.port, err)
	} else {
		t.log.Info("Listening peers on tcp://" + listener.Addr().String())
		t.port = listener.Addr().(*net.TCPAddr).Port
		t.portC <- t.port
		t.acceptor = acceptor.New(listener, t.incomingConnC, t.log)
		go t.acceptor.Run()
	}
}

func (t *torrent) startInfoDownloaders() {
	if t.info != nil {
		return
	}
	for len(t.infoDownloaders)-len(t.infoDownloadersSnubbed) < t.session.config.ParallelMetadataDownloads {
		id := t.nextInfoDownload()
		if id == nil {
			break
		}
		pe := id.Peer.(*peer.Peer)
		t.infoDownloaders[pe] = id
		id.RequestBlocks(t.maxAllowedRequests(pe))
		id.Peer.(*peer.Peer).ResetSnubTimer()
	}
}

func (t *torrent) startPieceDownloaders() {
	if t.status() != Downloading {
		return
	}
	for _, src := range t.webseedSources {
		if !src.Downloading() && !src.Disabled {
			started := t.startPieceDownloaderForWebseed(src)
			if !started {
				break
			}
		}
	}
	for pe := range t.peers {
		if !pe.Downloading {
			t.startPieceDownloaderFor(pe)
		}
	}
}

func (t *torrent) startPieceDownloaderForWebseed(src *webseedsource.WebseedSource) (started bool) {
	if t.webseedActiveDownloads >= t.session.config.WebseedMaxDownloads {
		return false
	}
	if t.status() != Downloading {
		return false
	}
	sp := t.piecePicker.PickWebseed(src)
	if sp == nil {
		return false
	}
	t.startWebseedDownloader(sp)
	t.webseedActiveDownloads++
	return true
}

func (t *torrent) startWebseedDownloader(sp *piecepicker.WebseedDownloadSpec) {
	t.log.Debugf("downloading pieces %d-%d from webseed %s", sp.Begin, sp.End, sp.Source.URL)
	ud := urldownloader.New(sp.Source.URL, sp.Begin, sp.End)
	for _, src := range t.webseedSources {
		if src != sp.Source {
			continue
		}
		if src.Downloader != nil {
			panic("already downloading from same url source")
		}
		src.Downloader = ud
		src.Disabled = false
		src.LastError = nil
		src.DownloadSpeed = metrics.NewMeter()
		break
	}
	go ud.Run(t.webseedClient, t.pieces, len(t.info.Files) > 1, t.webseedPieceResultC.SendC(), t.piecePool, t.session.config.WebseedResponseBodyReadTimeout)
}

func (t *torrent) startPieceDownloaderFor(pe *peer.Peer) {
	if t.status() != Downloading {
		return
	}
	if t.session.ram == nil {
		t.startSinglePieceDownloader(pe)
		return
	}
	ok := t.session.ram.Request(t.id, pe, int64(t.info.PieceLength), t.ramNotifyC, pe.Done())
	if ok {
		t.startSinglePieceDownloader(pe)
	}
}

func (t *torrent) startSinglePieceDownloader(pe *peer.Peer) {
	var started bool
	defer func() {
		if !started && t.session.ram != nil {
			t.session.ram.Release(int64(t.info.PieceLength))
		}
	}()
	if t.status() != Downloading {
		return
	}
	pi, allowedFast := t.piecePicker.PickFor(pe)
	if pi == nil {
		return
	}
	pd := piecedownloader.New(pi, pe, allowedFast, t.piecePool.Get(int(pi.Length)))
	if _, ok := t.pieceDownloaders[pe]; ok {
		panic("peer already has a piece downloader")
	}
	t.log.Debugf("requesting piece #%d from peer %s", pi.Index, pe.IP())
	t.pieceDownloaders[pe] = pd
	pe.Downloading = true
	pd.RequestBlocks(t.maxAllowedRequests(pe))
	pe.ResetSnubTimer()
	started = true
}

func (t *torrent) maxAllowedRequests(pe *peer.Peer) int {
	ret := t.session.config.DefaultRequestsOut
	if pe.ExtensionHandshake != nil && pe.ExtensionHandshake.RequestQueue > 0 {
		ret = pe.ExtensionHandshake.RequestQueue
	}
	if ret > t.session.config.MaxRequestsOut {
		ret = t.session.config.MaxRequestsOut
	}
	return ret
}
