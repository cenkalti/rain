package torrent

import (
	"errors"
	"fmt"

	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/urldownloader"
)

func (t *torrent) handlePieceWriteDone(pw *piecewriter.PieceWriter) {
	pw.Piece.Writing = false

	t.pieceMessagesC.Resume()
	t.webseedPieceResultC.Resume()

	pw.Buffer.Release()

	if !pw.HashOK {
		t.bytesWasted.Inc(int64(len(pw.Buffer.Data)))
		switch src := pw.Source.(type) {
		case *peer.Peer:
			t.log.Debugln("received corrupt piece from peer", src.String())
			t.closePeer(src)
			t.bannedPeerIPs[src.IP()] = struct{}{}
		case *urldownloader.URLDownloader:
			t.log.Debugln("received corrupt piece from webseed", src.URL)
			t.disableSource(src.URL, errors.New("corrupt piece"), false)
		default:
			panic("unhandled piece source")
		}
		t.startPieceDownloaders()
		return
	}
	if pw.Error != nil {
		t.stop(pw.Error)
		return
	}

	pw.Piece.Done = true
	if t.bitfield.Test(pw.Piece.Index) {
		panic(fmt.Sprintf("already have the piece #%d", pw.Piece.Index))
	}
	t.mBitfield.Lock()
	t.bitfield.Set(pw.Piece.Index)
	t.mBitfield.Unlock()

	if t.piecePicker != nil {
		_, ok := pw.Source.(*urldownloader.URLDownloader)
		src := t.piecePicker.RequestedWebseedSource(pw.Piece.Index)
		if !ok && src != nil {
			closed := t.piecePicker.WebseedStopAt(src, pw.Piece.Index)
			if closed {
				t.log.Debugf("closed webseed downloader: %s", src.URL)
				t.startPieceDownloaderForWebseed(src)
			}
		}

		for _, pe := range t.piecePicker.RequestedPeers(pw.Piece.Index) {
			pd2 := t.pieceDownloaders[pe]
			t.closePieceDownloader(pd2)
			pd2.CancelPending()
			t.startPieceDownloaderFor(pe)
		}
	}

	// Tell everyone that we have this piece
	for pe := range t.peers {
		t.updateInterestedState(pe)
		if pe.Bitfield.Test(pw.Piece.Index) {
			// Skip peers having the piece to save bandwidth
			continue
		}
		msg := peerprotocol.HaveMessage{Index: pw.Piece.Index}
		pe.SendMessage(msg)
	}

	completed := t.checkCompletion()
	if completed {
		t.log.Info("download completed")
		err := t.writeBitfield()
		if err != nil {
			t.stop(err)
		} else if t.stopAfterDownload {
			t.stop(nil)
		}
	}
}
