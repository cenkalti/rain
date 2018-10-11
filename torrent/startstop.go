package torrent

import (
	"net"
	"time"

	"github.com/cenkalti/rain/torrent/internal/acceptor"
	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
)

func (t *Torrent) start() {
	if t.running() {
		return
	}
	t.log.Info("starting torrent")
	t.errC = make(chan error, 1)

	// TODO do not run additional goroutines for writing piece data
	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(t.writeRequestC, t.writeResponseC, t.log)
		t.pieceWriters = append(t.pieceWriters, w)
		go w.Run()
	}

	t.startUnchokeTimers()

	if t.info != nil {
		t.allocator = allocator.New(t.info, t.storage, t.allocatorProgressC, t.allocatorResultC)
		go t.allocator.Run()
	} else {
		t.startAcceptor()
		t.startAnnouncers()
		t.startInfoDownloaders()
	}
}

func (t *Torrent) startAnnouncers() {
	if len(t.announcers) > 0 {
		return
	}
	for _, tr := range t.trackersInstances {
		an := announcer.New(tr, t.announcerRequests, t.completeC, t.addrsFromTrackers, t.log)
		t.announcers = append(t.announcers, an)
		go an.Run()
	}
}

func (t *Torrent) startAcceptor() {
	if t.acceptor != nil {
		return
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: t.port})
	if err != nil {
		t.log.Warningf("cannot listen port %d: %s", t.port, err)
	} else {
		t.log.Notice("Listening peers on tcp://" + listener.Addr().String())
		t.port = listener.Addr().(*net.TCPAddr).Port
		t.acceptor = acceptor.New(listener, t.newInConnC, t.log)
		go t.acceptor.Run()
	}
}

func (t *Torrent) stopAcceptor() {
	if t.acceptor != nil {
		t.acceptor.Close()
	}
	t.acceptor = nil
}

func (t *Torrent) startUnchokeTimers() {
	if t.unchokeTimer == nil {
		t.unchokeTimer = time.NewTicker(10 * time.Second)
		t.unchokeTimerC = t.unchokeTimer.C
	}
	if t.optimisticUnchokeTimer == nil {
		t.optimisticUnchokeTimer = time.NewTicker(30 * time.Second)
		t.optimisticUnchokeTimerC = t.optimisticUnchokeTimer.C
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

func (t *Torrent) startInfoDownloaders() {
	if t.info != nil {
		return
	}
	for len(t.infoDownloads) < parallelInfoDownloads {
		id := t.nextInfoDownload()
		if id == nil {
			break
		}
		t.log.Debugln("downloading info from", id.Peer.String())
		t.infoDownloads[id.Peer] = id
		t.connectedPeers[id.Peer].InfoDownloader = id
		go id.Run()
	}
}

func (t *Torrent) stopInfoDownloaders() {
	for id := range t.infoDownloads {
		id.Close()
	}
	t.infoDownloads = make(map[*peerconn.Conn]*infodownloader.InfoDownloader)
}

func (t *Torrent) startPieceDownloaders() {
	if t.bitfield == nil {
		return
	}
	for len(t.pieceDownloads) < parallelPieceDownloads {
		// TODO check status of existing downloads
		pd := t.nextPieceDownload()
		if pd == nil {
			break
		}
		t.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
		t.pieceDownloads[pd.Peer] = pd
		t.pieces[pd.Piece.Index].RequestedPeers[pd.Peer] = pd
		t.connectedPeers[pd.Peer].Downloader = pd
		go pd.Run()
	}
}

func (t *Torrent) stopPiecedownloaders() {
	for pd := range t.pieceDownloads {
		pd.Close()
	}
	t.pieceDownloads = make(map[*peerconn.Conn]*piecedownloader.PieceDownloader)
}

func (t *Torrent) stop(err error) {
	if !t.running() {
		return
	}
	t.log.Info("stopping torrent")
	if err != nil {
		t.errC <- err
	}
	t.errC = nil

	t.log.Debugln("stopping acceptor")
	t.stopAcceptor()

	t.log.Debugln("closing peer connections")
	for p := range t.connectedPeers {
		p.Close()
	}

	if t.data != nil {
		t.data.Close()
	}

	t.connectedPeers = make(map[*peerconn.Conn]*peer.Peer)
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
