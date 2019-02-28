package torrent

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/infodownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/mse"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerconn"
	"github.com/cenkalti/rain/internal/peerprotocol"
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

// Torrent event loop
func (t *torrent) run() {
	t.seedDurationTicker = time.NewTicker(time.Second)
	defer t.seedDurationTicker.Stop()

	t.speedCounterTicker = time.NewTicker(5 * time.Second)
	defer t.speedCounterTicker.Stop()

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
		case <-t.announcersStoppedC:
			t.stoppedEventAnnouncer = nil
			t.errC <- t.lastError
			t.errC = nil
			t.portC = nil
			t.log.Info("torrent has stopped")
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
		case p := <-t.allocatorProgressC:
			t.bytesAllocated = p.AllocatedSize
		case al := <-t.allocatorResultC:
			t.handleAllocationDone(al)
		case p := <-t.verifierProgressC:
			t.checkedPieces = p.Checked
		case ve := <-t.verifierResultC:
			t.handleVerificationDone(ve)
		case data := <-t.ramNotifyC:
			t.startSinglePieceDownloader(data.(*peer.Peer))
		case addrs := <-t.addrsFromTrackers:
			t.handleNewPeers(addrs, peer.SourceTracker)
		case addrs := <-t.addPeersCommandC:
			t.handleNewPeers(addrs, peer.SourceManual)
		case addrs := <-t.dhtPeersC:
			t.handleNewPeers(addrs, peer.SourceDHT)
		case trackers := <-t.addTrackersCommandC:
			t.trackers = append(t.trackers, trackers...)
			status := t.status()
			if status != Stopping && status != Stopped {
				for _, tr := range trackers {
					t.startNewAnnouncer(tr)
				}
			}
		case conn := <-t.incomingConnC:
			if len(t.incomingHandshakers)+len(t.incomingPeers) >= t.config.MaxPeerAccept {
				t.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			ip := conn.RemoteAddr().(*net.TCPAddr).IP
			ipstr := ip.String()
			if t.blocklist != nil && t.blocklist.Blocked(ip) {
				t.log.Debugln("peer is blocked:", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			if _, ok := t.connectedPeerIPs[ipstr]; ok {
				t.log.Debugln("received duplicate connection from same IP: ", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.New(conn)
			t.incomingHandshakers[h] = struct{}{}
			t.connectedPeerIPs[ipstr] = struct{}{}
			go h.Run(
				t.peerID,
				t.getSKey,
				t.checkInfoHash,
				t.incomingHandshakerResultC,
				t.config.PeerHandshakeTimeout,
				ourExtensions,
				t.config.ForceIncomingEncryption,
			)
		case req := <-t.announcerRequestC:
			tr := t.announcerFields()
			select {
			case req.Response <- announcer.Response{Torrent: tr}:
			case <-req.Cancel:
			}
		case pw := <-t.pieceWriterResultC:
			pw.Piece.Writing = false

			t.pieceMessagesC.Resume()

			pw.Buffer.Release()

			if !pw.HashOK {
				t.resumerStats.BytesWasted += int64(len(pw.Buffer.Data))
				t.log.Errorln("received corrupt piece from", pw.Peer.String())
				t.closePeer(pw.Peer)
				t.startPieceDownloaders()
				break
			}
			if pw.Error != nil {
				t.stop(pw.Error)
				break
			}

			pw.Piece.Done = true
			if t.bitfield.Test(pw.Piece.Index) {
				panic("already have the piece")
			}
			t.bitfield.Set(pw.Piece.Index)

			if t.piecePicker != nil {
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
			if t.resume != nil {
				if completed {
					t.writeBitfield(true)
				} else {
					t.deferWriteBitfield()
				}
			}
		case <-t.resumeWriteTimerC:
			t.writeBitfield(true)
		case now := <-t.seedDurationTicker.C:
			t.updateSeedDuration(now)
		case <-t.speedCounterTicker.C:
			t.downloadSpeed.Tick()
			t.uploadSpeed.Tick()
		case pe := <-t.peerSnubbedC:
			// Mark slow peer as snubbed and don't select that peer in piece picker
			pe.Snubbed = true
			if pd, ok := t.pieceDownloaders[pe]; ok {
				if _, ok := t.pieceDownloadersChoked[pe]; ok {
					panic("piece download is choked")
				}
				t.pieceDownloadersSnubbed[pe] = pd
				if t.piecePicker != nil {
					t.piecePicker.HandleSnubbed(pe, pd.Piece.Index)
				}
				t.startPieceDownloaders()
			} else if id, ok := t.infoDownloaders[pe]; ok {
				t.infoDownloadersSnubbed[pe] = id
				t.startInfoDownloaders()
			}
		case <-t.unchokeTimerC:
			t.tickUnchoke()
		case <-t.optimisticUnchokeTimerC:
			t.tickOptimisticUnchoke()
		case ih := <-t.incomingHandshakerResultC:
			delete(t.incomingHandshakers, ih)
			if ih.Error != nil {
				delete(t.connectedPeerIPs, ih.Conn.RemoteAddr().(*net.TCPAddr).IP.String())
				break
			}
			log := logger.New("peer <- " + ih.Conn.RemoteAddr().String())
			pe := peerconn.New(ih.Conn, log, t.config.PieceReadTimeout, t.config.PeerReadBufferSize)
			t.startPeer(pe, peer.SourceIncoming, t.incomingPeers, ih.PeerID, ih.Extensions, ih.Cipher)
		case oh := <-t.outgoingHandshakerResultC:
			delete(t.outgoingHandshakers, oh)
			if oh.Error != nil {
				delete(t.connectedPeerIPs, oh.Addr.IP.String())
				t.dialAddresses()
				break
			}
			log := logger.New("peer -> " + oh.Conn.RemoteAddr().String())
			pe := peerconn.New(oh.Conn, log, t.config.PieceReadTimeout, t.config.PeerReadBufferSize)
			t.startPeer(pe, oh.Source, t.outgoingPeers, oh.PeerID, oh.Extensions, oh.Cipher)
		case pe := <-t.peerDisconnectedC:
			t.closePeer(pe)
		case pm := <-t.pieceMessagesC.ReceiveC():
			t.handlePieceMessage(pm.(peer.PieceMessage))
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}

func (t *torrent) deferWriteBitfield() {
	if t.resumeWriteTimer == nil {
		t.resumeWriteTimer = time.NewTimer(t.config.BitfieldWriteInterval)
		t.resumeWriteTimerC = t.resumeWriteTimer.C
	}
}

func (t *torrent) writeBitfield(stopOnError bool) {
	if t.resumeWriteTimer != nil {
		t.resumeWriteTimer.Stop()
		t.resumeWriteTimer = nil
		t.resumeWriteTimerC = nil
	}
	err := t.resume.WriteBitfield(t.bitfield.Bytes())
	if err != nil {
		err = fmt.Errorf("cannot write bitfield to resume db: %s", err)
		t.log.Errorln(err)
		if stopOnError {
			t.stop(err)
		}
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
	delete(t.peersUnchoked, pe)
	delete(t.peersUnchokedOptimistic, pe)
	delete(t.incomingPeers, pe)
	delete(t.outgoingPeers, pe)
	delete(t.peerIDs, pe.ID)
	delete(t.connectedPeerIPs, pe.Conn.IP())
	if t.piecePicker != nil {
		t.piecePicker.HandleDisconnect(pe)
	}
	t.pexDropPeer(pe.Addr())
	t.dialAddresses()
}

func (t *torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
	_, open := t.pieceDownloaders[pd.Peer]
	if !open {
		return
	}
	delete(t.pieceDownloaders, pd.Peer)
	delete(t.pieceDownloadersSnubbed, pd.Peer)
	delete(t.pieceDownloadersChoked, pd.Peer)
	if t.piecePicker != nil {
		t.piecePicker.HandleCancelDownload(pd.Peer, pd.Piece.Index)
	}
	pd.Peer.Downloading = false
	if t.ram != nil {
		t.ram.Release(int64(t.info.PieceLength))
	}
}

func (t *torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	delete(t.infoDownloaders, id.Peer)
	delete(t.infoDownloadersSnubbed, id.Peer)
}

func (t *torrent) handleNewPeers(addrs []*net.TCPAddr, source peer.Source) {
	t.log.Debugf("received %d peers from %s", len(addrs), source)
	t.setNeedMorePeers(false)
	if status := t.status(); status == Stopped || status == Stopping {
		return
	}
	if !t.completed {
		t.addrList.Push(addrs, source)
		t.dialAddresses()
	}
}

func (t *torrent) dialAddresses() {
	if t.completed {
		return
	}
	peersConnected := func() int {
		return len(t.outgoingPeers) + len(t.outgoingHandshakers)
	}
	for peersConnected() < t.config.MaxPeerDial {
		addr, src := t.addrList.Pop()
		if addr == nil {
			t.setNeedMorePeers(true)
			return
		}
		ip := addr.IP.String()
		if _, ok := t.connectedPeerIPs[ip]; ok {
			continue
		}
		h := outgoinghandshaker.New(addr, src)
		t.outgoingHandshakers[h] = struct{}{}
		t.connectedPeerIPs[ip] = struct{}{}
		go h.Run(
			t.config.PeerConnectTimeout,
			t.config.PeerHandshakeTimeout,
			t.peerID,
			t.infoHash,
			t.outgoingHandshakerResultC,
			ourExtensions,
			t.config.DisableOutgoingEncryption,
			t.config.ForceOutgoingEncryption,
		)
	}
}

func (t *torrent) setNeedMorePeers(val bool) {
	for _, an := range t.announcers {
		an.NeedMorePeers(val)
	}
	if t.dhtAnnouncer != nil {
		t.dhtAnnouncer.NeedMorePeers(val)
	}
}

// Process messages received while we don't have metadata yet.
func (t *torrent) processQueuedMessages() {
	for pe := range t.peers {
		for _, msg := range pe.Messages {
			pm := peer.Message{Peer: pe, Message: msg}
			t.handlePeerMessage(pm)
		}
		pe.Messages = nil
	}
}

func (t *torrent) startPeer(
	p *peerconn.Conn,
	source peer.Source,
	peers map[*peer.Peer]struct{},
	peerID [20]byte,
	extensions [8]byte,
	cipher mse.CryptoMethod,
) {
	t.pexAddPeer(p.Addr())
	_, ok := t.peerIDs[peerID]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", peerID)
		p.CloseConn()
		t.pexDropPeer(p.Addr())
		t.dialAddresses()
		return
	}
	t.peerIDs[peerID] = struct{}{}

	pe := peer.New(p, source, peerID, extensions, cipher, t.config.RequestTimeout)
	t.peers[pe] = struct{}{}
	peers[pe] = struct{}{}
	if t.info != nil {
		pe.Bitfield = bitfield.New(t.info.NumPieces)
	}
	go pe.Run(t.messages, t.pieceMessagesC.SendC(), t.peerSnubbedC, t.peerDisconnectedC)
	t.sendFirstMessage(pe)
	t.fastUnchoke(pe)
}

func (t *torrent) pexAddPeer(addr *net.TCPAddr) {
	if !t.config.PEXEnabled {
		return
	}
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Add(addr)
		}
	}
}

func (t *torrent) pexDropPeer(addr *net.TCPAddr) {
	if !t.config.PEXEnabled {
		return
	}
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Drop(addr)
		}
	}
}

func (t *torrent) sendFirstMessage(p *peer.Peer) {
	bf := t.bitfield
	if p.FastEnabled && bf != nil && bf.All() {
		msg := peerprotocol.HaveAllMessage{}
		p.SendMessage(msg)
	} else if p.FastEnabled && (bf == nil || bf != nil && bf.Count() == 0) {
		msg := peerprotocol.HaveNoneMessage{}
		p.SendMessage(msg)
	} else if bf != nil {
		bitfieldData := make([]byte, len(bf.Bytes()))
		copy(bitfieldData, bf.Bytes())
		msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
		p.SendMessage(msg)
	}
	var metadataSize uint32
	if t.info != nil {
		metadataSize = t.info.InfoSize
	}
	if p.ExtensionsEnabled {
		extHandshakeMsg := peerprotocol.NewExtensionHandshake(metadataSize, t.config.ExtensionHandshakeClientVersion, p.Addr().IP)
		msg := peerprotocol.ExtensionMessage{
			ExtendedMessageID: peerprotocol.ExtensionIDHandshake,
			Payload:           extHandshakeMsg,
		}
		p.SendMessage(msg)
	}
}

func (t *torrent) fastUnchoke(pe *peer.Peer) {
	if pe.ClientChoking && pe.PeerInterested && len(t.peersUnchoked) < t.config.UnchokedPeers {
		t.unchokePeer(pe, false)
	}
	if pe.ClientChoking && pe.PeerInterested && len(t.peersUnchokedOptimistic) < t.config.OptimisticUnchokedPeers {
		t.unchokePeer(pe, true)
	}
}

func (t *torrent) chokePeer(pe *peer.Peer) {
	if !pe.ClientChoking {
		pe.ClientChoking = true
		pe.OptimisticUnchoked = false
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg)
		delete(t.peersUnchoked, pe)
		delete(t.peersUnchokedOptimistic, pe)
	}
}

func (t *torrent) unchokePeer(pe *peer.Peer, optimistic bool) {
	if pe.ClientChoking {
		pe.ClientChoking = false
		pe.OptimisticUnchoked = optimistic
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg)
		if optimistic {
			t.peersUnchokedOptimistic[pe] = struct{}{}
		} else {
			t.peersUnchoked[pe] = struct{}{}
		}
	}
}

func (t *torrent) checkCompletion() bool {
	if t.completed {
		return true
	}
	if !t.bitfield.All() {
		return false
	}
	t.log.Info("download completed")
	t.completed = true
	close(t.completeC)
	for h := range t.outgoingHandshakers {
		h.Close()
	}
	t.outgoingHandshakers = make(map[*outgoinghandshaker.OutgoingHandshaker]struct{})
	for pe := range t.peers {
		if !pe.PeerInterested {
			t.closePeer(pe)
		}
	}
	t.addrList.Reset()
	for _, pd := range t.pieceDownloaders {
		t.closePieceDownloader(pd)
		pd.CancelPending()
	}
	t.piecePicker = nil
	t.updateSeedDuration(time.Now())
	return true
}
