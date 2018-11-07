package torrent

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"net"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
	"github.com/cenkalti/rain/torrent/internal/tracker/trackermanager"
)

var errClosed = errors.New("torrent is closed")

func (t *Torrent) close() {
	// Stop if running.
	t.stop(errClosed)

	// Maybe we are in "Stopping" state. Close "stopped" event announcer.
	if t.stoppedEventAnnouncer != nil {
		t.stoppedEventAnnouncer.Close()
	}

	// Close open tracker connections.
	for _, tr := range t.trackersInstances {
		trackermanager.DefaultTrackerManager.Release(tr)
	}
}

// Torrent event loop
func (t *Torrent) run() {
	defer close(t.doneC)
	defer t.close()
	for {
		select {
		case <-t.closeC:
			return
		case <-t.startCommandC:
			t.start()
		case <-t.stopCommandC:
			t.stop(nil)
		case <-t.announcersStoppedC:
			t.stoppedEventAnnouncer = nil
			t.errC <- t.lastError
			t.errC = nil
			t.log.Info("torrent has stopped")
		case cmd := <-t.notifyErrorCommandC:
			cmd.errCC <- t.errC
		case req := <-t.statsCommandC:
			req.Response <- t.stats()
		case <-t.allocatorProgressC:
			// TODO handle allocation progress
		case al := <-t.allocatorResultC:
			t.handleAllocationDone(al)
		case <-t.verifierProgressC:
			// TODO handle verification progress
		case ve := <-t.verifierResultC:
			t.handleVerificationDone(ve)
		case addrs := <-t.addrsFromTrackers:
			t.handleNewPeers(addrs, "tracker")
		case addrs := <-t.addPeersC:
			t.handleNewPeers(addrs, "manual")
		case addrs := <-t.dhtPeersC:
			t.handleNewPeers(addrs, "dht")
		case conn := <-t.incomingConnC:
			if len(t.incomingHandshakers)+len(t.incomingPeers) >= t.config.MaxPeerAccept {
				t.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			ip := conn.RemoteAddr().(*net.TCPAddr).IP.String()
			if _, ok := t.connectedPeerIPs[ip]; ok {
				t.log.Debugln("received duplicate connection from same IP: ", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.New(conn)
			t.incomingHandshakers[h] = struct{}{}
			t.connectedPeerIPs[ip] = struct{}{}
			go h.Run(t.peerID, t.getSKey, t.checkInfoHash, t.incomingHandshakerResultC, t.config.PeerHandshakeTimeout, ourExtensions, t.config.ForceIncomingEncryption)
		case req := <-t.announcerRequestC:
			tr := t.announcerFields()
			// TODO set bytes uploaded/downloaded
			req.Response <- announcer.Response{Torrent: tr}
		case id := <-t.infoDownloaderResultC:
			t.closeInfoDownloader(id)
			if id.Error != nil {
				id.Peer.Logger().Error(id.Error)
				t.closePeer(id.Peer)
				t.startInfoDownloaders()
				break
			}
			hash := sha1.New()                              // nolint: gosec
			hash.Write(id.Bytes)                            // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), t.infoHash[:]) { // nolint: gosec
				id.Peer.Logger().Errorln("received info does not match with hash")
				t.closePeer(id.Peer)
				t.startInfoDownloaders()
				break
			}
			t.stopInfoDownloaders()

			var err error
			t.info, err = metainfo.NewInfo(id.Bytes)
			if err != nil {
				err = fmt.Errorf("cannot parse info bytes: %s", err)
				t.log.Error(err)
				t.stop(err)
				break
			}
			if t.info.Private == 1 {
				err = errors.New("private torrent from magnet")
				t.log.Error(err)
				t.stop(err)
				break
			}
			if t.resume != nil {
				err = t.resume.WriteInfo(t.info.Bytes)
				if err != nil {
					err = fmt.Errorf("cannot write resume info: %s", err)
					t.log.Error(err)
					t.stop(err)
					break
				}
			}
			t.startAllocator()
		case pd := <-t.pieceDownloaderResultC:
			t.log.Debugln("piece download completed. index:", pd.Piece.Index)
			t.closePieceDownloader(pd)
			if pd.Error != nil {
				// TODO handle corrupt piece
				// TODO stop on write error
				t.log.Errorln(pd.Error)
				t.closePeer(pd.Peer)
				t.startPieceDownloaders()
				break
			}
			for _, pd := range t.pieces[pd.Piece.Index].RequestedPeers {
				t.closePieceDownloader(pd)
				pd.CancelPending()
			}
			if t.bitfield.Test(pd.Piece.Index) {
				panic("already have the piece")
			}
			t.bitfield.Set(pd.Piece.Index)
			if t.resume != nil {
				err := t.resume.WriteBitfield(t.bitfield.Bytes())
				if err != nil {
					err = fmt.Errorf("cannot write bitfield to resume db: %s", err)
					t.log.Errorln(err)
					t.stop(err)
					break
				}
			}
			t.checkCompletion()
			// Tell everyone that we have this piece
			for pe := range t.peers {
				msg := peerprotocol.HaveMessage{Index: pd.Piece.Index}
				pe.SendMessage(msg)
				t.updateInterestedState(pe)
			}
			t.startPieceDownloaders()
		case pd := <-t.snubbedPieceDownloaderC:
			// Mark slow peer as snubbed and don't select that peer in piece picker
			pd.Peer.Snubbed = true
			t.peersSnubbed[pd.Peer] = struct{}{}
			t.pieceDownloadersSnubbed[pd.Peer] = pd
			t.startPieceDownloaders()
		case id := <-t.snubbedInfoDownloaderC:
			id.Peer.Snubbed = true
			t.peersSnubbed[id.Peer] = struct{}{}
			t.infoDownloadersSnubbed[id.Peer] = id
			t.startInfoDownloaders()
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
			pe := peerconn.New(ih.Conn, ih.PeerID, ih.Extensions, log, t.config.PieceTimeout)
			t.startPeer(pe, t.incomingPeers)
		case oh := <-t.outgoingHandshakerResultC:
			delete(t.outgoingHandshakers, oh)
			if oh.Error != nil {
				delete(t.connectedPeerIPs, oh.Addr.IP.String())
				t.dialAddresses()
				break
			}
			log := logger.New("peer -> " + oh.Conn.RemoteAddr().String())
			pe := peerconn.New(oh.Conn, oh.PeerID, oh.Extensions, log, t.config.PieceTimeout)
			t.startPeer(pe, t.outgoingPeers)
		case pe := <-t.peerDisconnectedC:
			t.closePeer(pe)
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}

func (t *Torrent) closePeer(pe *peer.Peer) {
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
	delete(t.peersSnubbed, pe)
	delete(t.peerIDs, pe.ID())
	delete(t.connectedPeerIPs, pe.Conn.IP())
	for i := range t.pieces {
		delete(t.pieces[i].HavingPeers, pe)
		delete(t.pieces[i].AllowedFastPeers, pe)
		delete(t.pieces[i].RequestedPeers, pe)
	}
	t.pexDropPeer(pe.Addr())
	t.dialAddresses()
}

func (t *Torrent) closePieceDownloader(pd *piecedownloader.PieceDownloader) {
	pd.Close()
	delete(t.pieceDownloaders, pd.Peer)
	delete(t.pieceDownloadersSnubbed, pd.Peer)
	delete(t.pieceDownloadersChoked, pd.Peer)
	delete(t.pieces[pd.Piece.Index].RequestedPeers, pd.Peer)
}

func (t *Torrent) closeInfoDownloader(id *infodownloader.InfoDownloader) {
	id.Close()
	delete(t.infoDownloaders, id.Peer)
	delete(t.infoDownloadersSnubbed, id.Peer)
}

func (t *Torrent) handleNewPeers(addrs []*net.TCPAddr, source string) {
	t.log.Debugf("received %d peers from %s", len(addrs), source)
	if !t.completed {
		t.addrList.Push(addrs, t.port)
		t.dialAddresses()
	}
}

func (t *Torrent) dialAddresses() {
	if t.completed {
		return
	}
	for len(t.outgoingPeers)+len(t.outgoingHandshakers) < t.config.MaxPeerDial {
		addr := t.addrList.Pop()
		if addr == nil {
			t.needMorePeers()
			break
		}
		ip := addr.IP.String()
		if _, ok := t.connectedPeerIPs[ip]; ok {
			continue
		}
		h := outgoinghandshaker.New(addr)
		t.outgoingHandshakers[h] = struct{}{}
		t.connectedPeerIPs[ip] = struct{}{}
		go h.Run(t.config.PeerConnectTimeout, t.config.PeerHandshakeTimeout, t.peerID, t.infoHash, t.outgoingHandshakerResultC, ourExtensions, t.config.DisableOutgoingEncryption, t.config.ForceOutgoingEncryption)
	}
}

func (t *Torrent) needMorePeers() {
	for _, an := range t.announcers {
		an.Trigger()
	}
	// TODO announce to DHT when need more peers
}

// Process messages received while we don't have metadata yet.
func (t *Torrent) processQueuedMessages() {
	for pe := range t.peers {
		for _, msg := range pe.Messages {
			pm := peer.Message{Peer: pe, Message: msg}
			t.handlePeerMessage(pm)
		}
	}
}

func (t *Torrent) startPeer(p *peerconn.Conn, peers map[*peer.Peer]struct{}) {
	t.pexAddPeer(p.Addr())
	_, ok := t.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.CloseConn()
		t.pexDropPeer(p.Addr())
		t.dialAddresses()
		return
	}
	t.peerIDs[p.ID()] = struct{}{}

	pe := peer.New(p)
	t.peers[pe] = struct{}{}
	peers[pe] = struct{}{}
	go pe.Run(t.messages, t.peerDisconnectedC)

	t.sendFirstMessage(pe)
	if len(t.peers) <= 4 {
		t.unchokePeer(pe)
	}
}

func (t *Torrent) pexAddPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Add(addr)
		}
	}
}

func (t *Torrent) pexDropPeer(addr *net.TCPAddr) {
	for pe := range t.peers {
		if pe.PEX != nil {
			pe.PEX.Drop(addr)
		}
	}
}

func (t *Torrent) sendFirstMessage(p *peer.Peer) {
	bf := t.bitfield
	if p.FastExtension && bf != nil && bf.All() {
		msg := peerprotocol.HaveAllMessage{}
		p.SendMessage(msg)
	} else if p.FastExtension && (bf == nil || bf != nil && bf.Count() == 0) {
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
	extHandshakeMsg := peerprotocol.NewExtensionHandshake(metadataSize)
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: peerprotocol.ExtensionIDHandshake,
		Payload:           extHandshakeMsg,
	}
	p.SendMessage(msg)
}

func (t *Torrent) chokePeer(pe *peer.Peer) {
	if !pe.AmChoking {
		pe.AmChoking = true
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg)
	}
}

func (t *Torrent) unchokePeer(pe *peer.Peer) {
	if pe.AmChoking {
		pe.AmChoking = false
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg)
	}
}

func (t *Torrent) checkCompletion() {
	if t.completed {
		return
	}
	if t.bitfield.All() {
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
	}
}
