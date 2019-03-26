package torrent

import (
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/urldownloader"
)

func (t *torrent) handleWebseedPieceResult(msg *urldownloader.PieceResult) {
	if msg.Error != nil {
		t.log.Errorln("webseed download error:", msg.Error)
		// TODO handle error
		return
	}

	t.log.Debugln("piece download completed. index:", msg.Index)

	piece := &t.pieces[msg.Index]

	t.resumerStats.BytesDownloaded += int64(len(msg.Buffer.Data))

	// TODO update download speed
	// t.downloadSpeed.Update(int64(len(msg.Buffer.Data)))

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
