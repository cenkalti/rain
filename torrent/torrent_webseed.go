package torrent

import (
	"time"

	"github.com/cenkalti/rain/v2/internal/piecewriter"
	"github.com/cenkalti/rain/v2/internal/urldownloader"
	"github.com/cenkalti/rain/v2/internal/webseedsource"
)

func (t *torrent) handleWebseedPieceResult(msg *urldownloader.PieceResult) {
	if msg.Error != nil {
		t.log.Debugln("webseed download error:", msg.Error)
		// Possible causes:
		// * Client.Do error
		// * Unexpected status code
		// * Response.Body.Read error
		t.disableSource(msg.Downloader.URL, msg.Error, true)
		t.webseedActiveDownloads--
		t.startPieceDownloaders()
		return
	}

	piece := &t.pieces[msg.Index]

	// The piece may have already been completed by a peer that was downloading
	// it concurrently: webseed ranges and peer downloads can overlap, and when
	// a peer finishes such a piece the webseed range is only truncated after
	// the fact (see WebseedStopAt). A result that was already in flight then
	// arrives for a piece that is now Done. Writing it again would trip the
	// "already have the piece" check in handlePieceWriteDone, so discard the
	// stale result. This mirrors handlePieceMessage, which drops blocks whose
	// downloader is no longer current.
	if piece.Done {
		t.log.Debugf("discarding already completed piece #%d from webseed %s", msg.Index, msg.Downloader.URL)
		t.bytesWasted.Inc(int64(len(msg.Buffer.Data)))
		msg.Buffer.Release()
		if msg.Done {
			for _, src := range t.webseedSources {
				// Match by downloader identity: the source may already have a
				// different (or no) downloader if it was closed elsewhere, in
				// which case there is nothing to clean up here.
				if src.Downloader != msg.Downloader {
					continue
				}
				t.closeWebseedDownloader(src)
				t.webseedActiveDownloads--
				t.startPieceDownloaderForWebseed(src)
				break
			}
		}
		return
	}

	t.log.Debugf("piece #%d downloaded from %s", msg.Index, msg.Downloader.URL)

	t.bytesDownloaded.Inc(int64(len(msg.Buffer.Data)))
	t.downloadSpeed.Mark(int64(len(msg.Buffer.Data)))
	for _, src := range t.webseedSources {
		if src.URL != msg.Downloader.URL {
			continue
		}
		src.DownloadSpeed.Mark(int64(len(msg.Buffer.Data)))
		break
	}

	if piece.Writing {
		t.crash("piece is already writing")
	}
	piece.Writing = true

	// Prevent receiving piece messages to avoid more than 1 write per torrent.
	t.pieceMessagesC.Suspend()
	t.webseedPieceResultC.Suspend()

	pw := piecewriter.New(piece, msg.Downloader, msg.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)

	if msg.Done {
		for _, src := range t.webseedSources {
			if src.URL != msg.Downloader.URL {
				continue
			}
			t.closeWebseedDownloader(src)
			t.webseedActiveDownloads--
			t.startPieceDownloaderForWebseed(src)
			break
		}
	}
}

func (t *torrent) disableSource(srcurl string, err error, retry bool) {
	for _, src := range t.webseedSources {
		if src.URL != srcurl {
			continue
		}
		src.Disabled = true
		src.LastError = err
		t.closeWebseedDownloader(src)
		if retry {
			go t.notifyWebseedRetry(src)
		}
		break
	}
}

func (t *torrent) notifyWebseedRetry(src *webseedsource.WebseedSource) {
	select {
	case <-time.After(time.Minute):
		select {
		case t.webseedRetryC <- src:
		case <-t.closeC:
		}
	case <-t.closeC:
	}
}
