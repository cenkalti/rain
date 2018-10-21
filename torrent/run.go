package torrent

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"net"

	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
)

var errClosed = errors.New("torrent is closed")

func (t *Torrent) close() {
	t.stop(errClosed)
	if t.stoppedEventAnnouncer != nil {
		t.stoppedEventAnnouncer.Close()
	}
	for _, tr := range t.trackersInstances {
		tr.Close()
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
		case n := <-t.uploadByteCounterC:
			t.bytesUploaded += n
		case <-t.allocatorProgressC:
			// TODO handle allocation progress
		case al := <-t.allocatorResultC:
			t.handleAllocationDone(al)
		case <-t.verifierProgressC:
			// TODO handle verification progress
		case ve := <-t.verifierResultC:
			t.handleVerificationDone(ve)
		case addrs := <-t.addrsFromTrackers:
			t.addrList.Push(addrs, t.port)
			t.dialAddresses()
		case conn := <-t.incomingConnC:
			if len(t.incomingHandshakers)+len(t.incomingPeers) >= Config.Download.MaxPeerAccept {
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
			go h.Run(t.peerID, t.getSKey, t.checkInfoHash, t.incomingHandshakerResultC, t.log, Config.Peer.HandshakeTimeout, Config.Peer.PieceTimeout, t.uploadByteCounterC)
		case req := <-t.announcerRequestC:
			tr := t.announcerFields()
			// TODO set bytes uploaded/downloaded
			req.Response <- announcer.Response{Transfer: tr}
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
			t.startPeer(ih.Peer, t.incomingPeers)
		case oh := <-t.outgoingHandshakerResultC:
			delete(t.outgoingHandshakers, oh)
			if oh.Error != nil {
				delete(t.connectedPeerIPs, oh.Addr.IP.String())
				t.dialAddresses()
				break
			}
			t.startPeer(oh.Peer, t.outgoingPeers)
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

func (t *Torrent) dialAddresses() {
	if t.completed {
		return
	}
	for len(t.outgoingPeers)+len(t.outgoingHandshakers) < Config.Download.MaxPeerDial {
		addr := t.addrList.Pop()
		if addr == nil {
			for _, an := range t.announcers {
				an.Trigger()
			}
			break
		}
		ip := addr.IP.String()
		if _, ok := t.connectedPeerIPs[ip]; ok {
			continue
		}
		h := outgoinghandshaker.New(addr)
		t.outgoingHandshakers[h] = struct{}{}
		t.connectedPeerIPs[ip] = struct{}{}
		go h.Run(Config.Peer.ConnectTimeout, Config.Peer.HandshakeTimeout, Config.Peer.PieceTimeout, t.peerID, t.infoHash, t.outgoingHandshakerResultC, t.log, t.uploadByteCounterC)
	}
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
	_, ok := t.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.CloseConn()
		t.dialAddresses()
		return
	}
	t.peerIDs[p.ID()] = struct{}{}

	pe := peer.New(p, t.messages, t.peerDisconnectedC)
	t.peers[pe] = struct{}{}
	peers[pe] = struct{}{}
	go pe.Run()

	t.sendFirstMessage(pe)
	if len(t.peers) <= 4 {
		t.unchokePeer(pe)
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
	extHandshakeMsg := peerprotocol.NewExtensionHandshake()
	if t.info != nil {
		extHandshakeMsg.MetadataSize = t.info.InfoSize
	}
	msg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: peerprotocol.ExtensionHandshakeID,
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
	}
}
