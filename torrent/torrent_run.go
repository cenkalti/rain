package torrent

import (
	"time"

	"github.com/cenkalti/rain/v2/internal/peersource"
)

// Torrent event loop
func (t *torrent) run() {
	t.seedDurationTicker = time.NewTicker(time.Second)
	defer t.seedDurationTicker.Stop()

	t.unchokeTicker = time.NewTicker(10 * time.Second)
	defer t.unchokeTicker.Stop()

	for {
		select {
		case <-t.closeC:
			t.close()
			close(t.doneC)
			return
		// Commands from the public API. Each carries its own behavior, so the
		// senders in torrent_commands.go are the list of what can arrive here.
		case f := <-t.commandC:
			f()
		case <-t.announcersStoppedC:
			t.handleStopped()
		case p := <-t.allocatorProgressC:
			t.bytesAllocated = p.AllocatedSize
		case al := <-t.allocatorResultC:
			t.handleAllocationDone(al)
		case p := <-t.verifierProgressC:
			t.checkedPieces = p.Checked
		case ve := <-t.verifierResultC:
			t.handleVerificationDone(ve)
		case data := <-t.ramNotifyC:
			t.startSinglePieceDownloader(data)
		case addrs := <-t.addrsFromTrackers:
			t.handleNewPeers(addrs, peersource.Tracker)
		case addrs := <-t.dhtPeersC:
			t.handleNewPeers(addrs, peersource.DHT)
		case conn := <-t.incomingConnC:
			t.handleNewConnection(conn)
		case res := <-t.webseedPieceResultC.ReceiveC():
			t.handleWebseedPieceResult(res)
		case src := <-t.webseedRetryC:
			t.startPieceDownloaderForWebseed(src)
		case pw := <-t.pieceWriterResultC:
			t.handlePieceWriteDone(pw)
		case now := <-t.seedDurationTicker.C:
			t.updateSeedDuration(now)
		case pe := <-t.peerSnubbedC:
			t.handlePeerSnubbed(pe)
		case <-t.unchokeTicker.C:
			t.unchoker.TickUnchoke(t.getPeersForUnchoker(), t.completed)
		case ih := <-t.incomingHandshakerResultC:
			t.handleIncomingHandshakeDone(ih)
		case oh := <-t.outgoingHandshakerResultC:
			t.handleOutgoingHandshakeDone(oh)
		case pe := <-t.peerDisconnectedC:
			t.closePeer(pe)
		case pm := <-t.pieceMessagesC.ReceiveC():
			t.handlePieceMessage(pm)
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}
