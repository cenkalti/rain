package torrent

import (
	"errors"

	"github.com/cenkalti/rain/internal/infodownloader"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piecedownloader"
)

var errClosed = errors.New("torrent is closed")

func (t *torrent) close() {
	// Stop if running.
	t.stop(errClosed)

	// Maybe we are in "Stopping" state. Close "stopped" event announcer.
	if t.stoppedEventAnnouncer != nil {
		t.stoppedEventAnnouncer.Close()
	}
}

func (t *torrent) closePeer(pe *peer.Peer) {
	pe.Close()
	if pd, ok := t.pieceDownloaders[pe]; ok {
		t.closePieceDownloader(pd)
	}
	if id, ok := t.infoDownloaders[pe]; ok {
		t.closeInfoDownloader(id)
	}
	delete(t.peers, pe)
	delete(t.incomingPeers, pe)
	delete(t.outgoingPeers, pe)
	delete(t.peerIDs, pe.ID)
	delete(t.connectedPeerIPs, pe.Conn.IP())
	if t.piecePicker != nil {
		t.piecePicker.HandleDisconnect(pe)
	}
	t.unchoker.HandleDisconnect(pe)
	t.pexDropPeer(pe.Addr())
	t.dialAddresses()
}

func (t *torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
	pe := pd.Peer.(*peer.Peer)
	_, open := t.pieceDownloaders[pe]
	if !open {
		return
	}
	delete(t.pieceDownloaders, pe)
	delete(t.pieceDownloadersSnubbed, pe)
	delete(t.pieceDownloadersChoked, pe)
	if t.piecePicker != nil {
		t.piecePicker.HandleCancelDownload(pe, pd.Piece.Index)
	}
	pe.Downloading = false
	if t.ram != nil {
		t.ram.Release(int64(t.info.PieceLength))
	}
}

func (t *torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	delete(t.infoDownloaders, id.Peer.(*peer.Peer))
	delete(t.infoDownloadersSnubbed, id.Peer.(*peer.Peer))
}
