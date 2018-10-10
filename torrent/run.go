package torrent

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/cenkalti/rain/torrent/internal/allocator"
	"github.com/cenkalti/rain/torrent/internal/announcer"
	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/torrent/internal/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecewriter"
	"github.com/cenkalti/rain/torrent/internal/tracker"
	"github.com/cenkalti/rain/torrent/internal/verifier"
)

func (t *Torrent) close() {
	t.stop(errors.New("torrent is closed"))

	t.log.Debugln("closing outgoing handshakers")
	for _, oh := range t.outgoingHandshakers {
		oh.Close()
	}

	t.log.Debugln("closing incoming handshakers")
	for _, ih := range t.incomingHandshakers {
		ih.Close()
	}

	t.log.Debugln("closing info downloaders")
	for _, id := range t.infoDownloads {
		id.Close()
	}

	t.log.Debugln("closing piece downloaders")
	for _, pd := range t.pieceDownloads {
		pd.Close()
	}

	t.log.Debugln("closing incoming peer connections")
	for ip := range t.incomingPeers {
		ip.Close()
	}

	t.log.Debugln("closing outgoin peer connections")
	for op := range t.outgoingPeers {
		op.Close()
	}

	// TODO close data
	// TODO order closes here
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
				t.pieceDownloaders.Start()
				t.startAcceptor()
				t.startAnnouncers()
				break
			}
			if !res.NeedHashCheck {
				t.bitfield = bitfield.New(t.info.NumPieces)
				t.processQueuedMessages()
				t.pieceDownloaders.Start()
				t.startAcceptor()
				t.startAnnouncers()
				break
			}
			if res.NeedHashCheck {
				t.verifier = verifier.New(t.data.Pieces, t.verifierProgressC, t.verifierResultC)
				go t.verifier.Run()
			}
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
			for _, pe := range t.connectedPeers {
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
			t.pieceDownloaders.Start()
			t.startAcceptor()
			t.startAnnouncers()
		case addrs := <-t.addrsFromTrackers:
			t.addrList.Push(addrs, t.port)
			t.dialAddresses()
		case conn := <-t.newInConnC:
			if len(t.incomingHandshakers)+len(t.incomingPeers) >= maxPeerAccept {
				t.log.Debugln("peer limit reached, rejecting peer", conn.RemoteAddr().String())
				conn.Close()
				break
			}
			h := incominghandshaker.NewIncoming(conn, t.peerID, t.sKeyHash, t.infoHash, t.incomingHandshakerResultC, t.log)
			t.incomingHandshakers[conn.RemoteAddr().String()] = h
			go h.Run()
		case req := <-t.announcerRequests:
			tr := tracker.Transfer{
				InfoHash: t.infoHash,
				PeerID:   t.peerID,
				Port:     t.port,
			}
			if t.bitfield == nil {
				tr.BytesLeft = math.MaxUint32
			} else {
				// TODO this is wrong, pre-calculate complete and incomplete bytes
				tr.BytesLeft = t.info.TotalLength - int64(t.info.PieceLength)*int64(t.bitfield.Count())
			}
			// TODO set bytes uploaded/downloaded
			req.Response <- announcer.Response{Transfer: tr}
		case <-t.infoDownloaders.Ready:
			if t.info != nil {
				t.infoDownloaders.Stop()
				break
			}
			if len(t.infoDownloads) >= parallelInfoDownloads {
				t.infoDownloaders.Stop()
				break
			}
			id := t.nextInfoDownload()
			if id == nil {
				t.infoDownloaders.Stop()
				break
			}
			t.log.Debugln("downloading info from", id.Peer.String())
			t.infoDownloads[id.Peer] = id
			t.connectedPeers[id.Peer].InfoDownloader = id
			go id.Run()
		case res := <-t.infoDownloaderResultC:
			t.connectedPeers[res.Peer].InfoDownloader = nil
			delete(t.infoDownloads, res.Peer)
			t.infoDownloaders.Signal(1)
			if res.Error != nil {
				res.Peer.Logger().Error(res.Error)
				res.Peer.Close()
				break
			}
			hash := sha1.New()                              // nolint: gosec
			hash.Write(res.Bytes)                           // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), t.infoHash[:]) { // nolint: gosec
				res.Peer.Logger().Errorln("received info does not match with hash")
				t.infoDownloaders.Signal(1)
				res.Peer.Close()
				break
			}
			t.infoDownloaders.Stop()

			var err error
			t.info, err = metainfo.NewInfo(res.Bytes)
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
			t.allocator = allocator.New(t.info, t.storage, t.allocatorProgressC, t.allocatorResultC)
			go t.allocator.Run()
		case <-t.pieceDownloaders.Ready:
			if t.bitfield == nil {
				t.pieceDownloaders.Stop()
				break
			}
			if len(t.pieceDownloads) >= parallelPieceDownloads {
				t.pieceDownloaders.Stop()
				break
			}
			// TODO check status of existing downloads
			pd := t.nextPieceDownload()
			if pd == nil {
				t.pieceDownloaders.Stop()
				break
			}
			t.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
			t.pieceDownloads[pd.Peer] = pd
			t.pieces[pd.Piece.Index].RequestedPeers[pd.Peer] = pd
			t.connectedPeers[pd.Peer].Downloader = pd
			go pd.Run()
		case res := <-t.pieceDownloaderResultC:
			t.log.Debugln("piece download completed. index:", res.Piece.Index)
			if pe, ok := t.connectedPeers[res.Peer]; ok {
				pe.Downloader = nil
			}
			delete(t.pieceDownloads, res.Peer)
			delete(t.pieces[res.Piece.Index].RequestedPeers, res.Peer)
			t.pieceDownloaders.Signal(1)
			ok := t.pieces[res.Piece.Index].Piece.Verify(res.Bytes)
			if !ok {
				// TODO handle corrupt piece
				break
			}
			t.writeRequestC <- piecewriter.Request{Piece: res.Piece, Data: res.Bytes}
			t.pieces[res.Piece.Index].Writing = true
		case resp := <-t.writeResponseC:
			t.pieces[resp.Request.Piece.Index].Writing = false
			if resp.Error != nil {
				err := fmt.Errorf("cannot write piece data: %s", resp.Error)
				t.log.Errorln(err)
				t.stop(err)
				break
			}
			t.bitfield.Set(resp.Request.Piece.Index)
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
			for _, pe := range t.connectedPeers {
				msg := peerprotocol.HaveMessage{Index: resp.Request.Piece.Index}
				pe.SendMessage(msg)
				t.updateInterestedState(pe)
			}
		case <-t.unchokeTimerC:
			peers := make([]*peer.Peer, 0, len(t.connectedPeers))
			for _, pe := range t.connectedPeers {
				if !pe.OptimisticUnhoked {
					peers = append(peers, pe)
				}
			}
			sort.Sort(peer.ByDownloadRate(peers))
			for _, pe := range t.connectedPeers {
				pe.BytesDownlaodedInChokePeriod = 0
			}
			unchokedPeers := make(map[*peer.Peer]struct{}, 3)
			for i, pe := range peers {
				if i == 3 {
					break
				}
				t.unchokePeer(pe)
				unchokedPeers[pe] = struct{}{}
			}
			for _, pe := range t.connectedPeers {
				if _, ok := unchokedPeers[pe]; !ok {
					t.chokePeer(pe)
				}
			}
		case <-t.optimisticUnchokeTimerC:
			peers := make([]*peer.Peer, 0, len(t.connectedPeers))
			for _, pe := range t.connectedPeers {
				if !pe.OptimisticUnhoked && pe.AmChoking {
					peers = append(peers, pe)
				}
			}
			if t.optimisticUnchokedPeer != nil {
				t.optimisticUnchokedPeer.OptimisticUnhoked = false
				t.chokePeer(t.optimisticUnchokedPeer)
			}
			if len(peers) == 0 {
				t.optimisticUnchokedPeer = nil
				break
			}
			pe := peers[rand.Intn(len(peers))]
			pe.OptimisticUnhoked = true
			t.unchokePeer(pe)
			t.optimisticUnchokedPeer = pe
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
			delete(t.peerIDs, pe.ID())
			if pe.Downloader != nil {
				pe.Downloader.Close()
				delete(t.pieceDownloads, pe.Conn)
			}
			if pe.InfoDownloader != nil {
				pe.InfoDownloader.Close()
				delete(t.infoDownloads, pe.Conn)
			}
			delete(t.connectedPeers, pe.Conn)
			for i := range t.pieces {
				delete(t.pieces[i].HavingPeers, pe.Conn)
				delete(t.pieces[i].AllowedFastPeers, pe.Conn)
				delete(t.pieces[i].RequestedPeers, pe.Conn)
			}
			delete(t.incomingPeers, pe.Conn)
			if _, ok := t.outgoingPeers[pe.Conn]; ok {
				delete(t.outgoingPeers, pe.Conn)
				t.dialAddresses()
			}
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}

func (t *Torrent) dialAddresses() {
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

func (t *Torrent) processQueuedMessages() {
	// process previously received messages
	for _, pe := range t.connectedPeers {
		for _, msg := range pe.Messages {
			pm := peer.Message{Peer: pe, Message: msg}
			t.handlePeerMessage(pm)
		}
	}
}

func (t *Torrent) startPeer(p *peerconn.Conn, peers map[*peerconn.Conn]struct{}) {
	_, ok := t.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.CloseConn()
		return
	}
	t.peerIDs[p.ID()] = struct{}{}

	pe := peer.New(p, t.messages, t.peerDisconnectedC)
	t.connectedPeers[p] = pe
	peers[p] = struct{}{}
	go pe.Run()

	t.sendFirstMessage(p)
	if len(t.connectedPeers) <= 4 {
		t.unchokePeer(pe)
	}
}

func (t *Torrent) sendFirstMessage(p *peerconn.Conn) {
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
		close(t.completeC)
	}
}
