package torrent

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
)

func (t *Torrent) close() {
	t.stop(errors.New("torrent is closed"))

	if t.data != nil {
		t.data.Close()
	}
}

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
			t.stop(errors.New("torrent is stopped"))
		case cmd := <-t.notifyErrorCommandC:
			cmd.errCC <- t.errC
		case req := <-t.statsCommandC:
			req.Response <- t.stats()
		case <-t.allocatorProgressC:
			// TODO handle allocation progress
		case res := <-t.allocatorResultC:
			t.allocator = nil
			if res.Error != nil {
				t.stop(fmt.Errorf("file allocation error: %s", res.Error))
				break
			}
			t.data = res.Data
			t.pieces = make([]*piece.Piece, len(t.data.Pieces))
			t.sortedPieces = make([]*piece.Piece, len(t.data.Pieces))
			for i := range t.data.Pieces {
				p := piece.New(&t.data.Pieces[i])
				t.pieces[i] = p
				t.sortedPieces[i] = p
			}
			if t.bitfield != nil {
				t.checkCompletion()
				t.processQueuedMessages()
				t.startAcceptor()
				t.startAnnouncers()
				t.startPieceDownloaders()
				t.startUnchokeTimers()
				break
			}
			if !res.NeedHashCheck {
				t.bitfield = bitfield.New(t.info.NumPieces)
				t.processQueuedMessages()
				t.startAcceptor()
				t.startAnnouncers()
				t.startPieceDownloaders()
				t.startUnchokeTimers()
				break
			}
			t.startVerifier()
		case <-t.verifierProgressC:
			// TODO handle verification progress
		case res := <-t.verifierResultC:
			t.verifier = nil
			if res.Error != nil {
				t.stop(fmt.Errorf("file verification error: %s", res.Error))
				break
			}
			t.bitfield = res.Bitfield
			if t.resume != nil {
				err := t.resume.WriteBitfield(t.bitfield.Bytes())
				if err != nil {
					t.stop(fmt.Errorf("cannot write bitfield to resume db: %s", err))
					break
				}
			}
			for pe := range t.peers {
				for i := uint32(0); i < t.bitfield.Len(); i++ {
					if t.bitfield.Test(i) {
						msg := peerprotocol.HaveMessage{Index: i}
						pe.SendMessage(msg)
					}
				}
				t.updateInterestedState(pe)
			}
			t.checkCompletion()
			t.processQueuedMessages()
			t.startAcceptor()
			t.startAnnouncers()
			t.startPieceDownloaders()
			t.startUnchokeTimers()
		case addrs := <-t.addrsFromTrackers:
			t.addrList.Push(addrs, t.port)
			t.dialAddresses()
		case conn := <-t.incomingConnC:
			if len(t.incomingHandshakers)+len(t.incomingPeers) >= maxPeerAccept {
				t.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.NewIncoming(conn, t.peerID, t.sKeyHash, t.infoHash, t.incomingHandshakerResultC, t.log)
			t.incomingHandshakers[conn.RemoteAddr().String()] = h
			go h.Run()
		case req := <-t.announcerRequestC:
			tr := t.announcerFields()
			// TODO set bytes uploaded/downloaded
			req.Response <- announcer.Response{Transfer: tr}
		case id := <-t.infoDownloaderDoneC:
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
		case pd := <-t.pieceDownloaderDoneC:
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
			if t.bitfield.Test(pd.Piece.Index) {
				panic("already have the piece")
			}
			t.bitfield.Set(pd.Piece.Index) // TODO set bits in piece downloader, make thread-safe
			if t.resume != nil {
				err := t.resume.WriteBitfield(t.bitfield.Bytes()) // TODO write bitfield in piece downloader
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
			t.pieceDownloadersSnubbed[pd.Peer] = pd
			t.startPieceDownloaders()
		case id := <-t.snubbedInfoDownloaderC:
			id.Peer.Snubbed = true
			t.infoDownloadersSnubbed[id.Peer] = id
			t.startInfoDownloaders()
		case <-t.unchokeTimerC:
			t.tickUnchoke()
		case <-t.optimisticUnchokeTimerC:
			t.tickOptimisticUnchoke()
		case res := <-t.incomingHandshakerResultC:
			delete(t.incomingHandshakers, res.Conn.RemoteAddr().String())
			if res.Error != nil {
				res.Conn.Close()
				break
			}
			t.startPeer(res.Peer, t.incomingPeers)
		case res := <-t.outgoingHandshakerResultC:
			delete(t.outgoingHandshakers, res.Addr.String())
			if res.Error != nil {
				t.dialAddresses()
				break
			}
			t.startPeer(res.Peer, t.outgoingPeers)
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
	delete(t.peerIDs, pe.ID())
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
	if t.completed() {
		return
	}
	for len(t.outgoingPeers)+len(t.outgoingHandshakers) < maxPeerDial {
		addr := t.addrList.Pop()
		if addr == nil {
			break
		}
		h := outgoinghandshaker.NewOutgoing(addr, t.peerID, t.infoHash, t.outgoingHandshakerResultC, t.log)
		t.outgoingHandshakers[addr.String()] = h
		go h.Run()
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
	if t.completed() {
		return
	}
	if t.bitfield.All() {
		t.log.Info("download completed")
		close(t.completeC)
		for pe := range t.peers {
			if !pe.PeerInterested {
				t.closePeer(pe)
			}
		}
	}
}
