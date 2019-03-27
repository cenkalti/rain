package torrent

import (
	"time"

	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/urldownloader"
	"github.com/cenkalti/rain/internal/webseedsource"
)

func (t *torrent) handleWebseedPieceResult(msg *urldownloader.PieceResult) {
	if msg.Error != nil {
		t.log.Errorln("webseed download error:", msg.Error)
		// Possible causes:
		// * Client.Do error
		// * Unexpected status code
		// * Response.Body.Read error
		t.disableSource(msg.Downloader.URL, msg.Error, true)
		return
	}

	piece := &t.pieces[msg.Index]

	t.resumerStats.BytesDownloaded += int64(len(msg.Buffer.Data))
	t.downloadSpeed.Update(int64(len(msg.Buffer.Data)))

	if piece.Writing {
		panic("piece is already writing")
	}
	piece.Writing = true

	// Prevent receiving piece messages to avoid more than 1 write per torrent.
	t.pieceMessagesC.Suspend()
	t.webseedPieceResultC.Suspend()

	pw := piecewriter.New(piece, msg.Downloader, msg.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC)
}

func (t *torrent) disableSource(srcurl string, err error, retry bool) {
	for _, src := range t.webseedSources {
		if src.URL != srcurl {
			continue
		}
		src.Disabled = true
		src.DisabledAt = time.Now()
		src.LastError = err
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
