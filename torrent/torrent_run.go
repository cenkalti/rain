package torrent

import (
	"time"

	"github.com/cenkalti/rain/internal/peersource"
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
		case <-t.startCommandC:
			t.start()
		case <-t.stopCommandC:
			t.stop(nil)
		case <-t.announceCommandC:
			t.setNeedMorePeers(true)
		case <-t.verifyCommandC:
			t.handleVerifyCommand()
		case <-t.announcersStoppedC:
			t.handleStopped()
		case cmd := <-t.notifyErrorCommandC:
			cmd.errCC <- t.errC
		case cmd := <-t.notifyListenCommandC:
			cmd.portCC <- t.portC
		case req := <-t.statsCommandC:
			req.Response <- t.stats()
		case req := <-t.trackersCommandC:
			req.Response <- t.getTrackers()
		case req := <-t.peersCommandC:
			req.Response <- t.getPeers()
		case req := <-t.webseedsCommandC:
			req.Response <- t.getWebseeds()
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
		case addrs := <-t.addPeersCommandC:
			t.handleNewPeers(addrs, peersource.Manual)
		case addrs := <-t.dhtPeersC:
			t.handleNewPeers(addrs, peersource.DHT)
		case trackers := <-t.addTrackersCommandC:
			t.handleNewTrackers(trackers)
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
