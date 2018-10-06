package torrent

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/announcer"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader/acceptor"
	"github.com/cenkalti/rain/internal/downloader/handshaker/incominghandshaker"
	"github.com/cenkalti/rain/internal/downloader/handshaker/outgoinghandshaker"
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/peer"
	"github.com/cenkalti/rain/internal/downloader/piece"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/metainfo"
	ip "github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/tracker"
)

func (t *Torrent) start() {
	if t.running {
		return
	}
	t.running = true

	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: t.port})
	if err != nil {
		t.log.Warningf("cannot listen port %d: %s", t.port, err)
	} else {
		t.log.Notice("Listening peers on tcp://" + listener.Addr().String())
		t.port = listener.Addr().(*net.TCPAddr).Port
		t.acceptor = acceptor.New(listener, t.newInConnC, t.log)
		go t.acceptor.Run()
	}

	for _, tr := range t.trackersInstances {
		an := announcer.New(tr, t.announcerRequests, t.completeC, t.addrsFromTrackers, t.log)
		t.announcers = append(t.announcers, an)
		go an.Run()
	}

	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(t.writeRequestC, t.writeResponseC, t.log)
		t.pieceWriters = append(t.pieceWriters, w)
		go w.Run()
	}

	t.unchokeTimer = time.NewTicker(10 * time.Second)
	t.unchokeTimerC = t.unchokeTimer.C

	t.optimisticUnchokeTimer = time.NewTicker(30 * time.Second)
	t.optimisticUnchokeTimerC = t.optimisticUnchokeTimer.C

	t.dialLimit.Start()

	t.pieceDownloaders.Start()
	t.infoDownloaders.Start()

	t.errC = make(chan error, 1)
}

func (t *Torrent) stop(err error) {
	if !t.running {
		return
	}
	t.running = false

	if t.acceptor != nil {
		t.acceptor.Close()
	}
	t.acceptor = nil

	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil

	for _, pw := range t.pieceWriters {
		pw.Close()
	}
	t.pieceWriters = nil

	t.unchokeTimer.Stop()
	t.unchokeTimerC = nil
	t.optimisticUnchokeTimer.Stop()
	t.optimisticUnchokeTimerC = nil

	t.dialLimit.Stop()

	t.pieceDownloaders.Stop()
	t.infoDownloaders.Stop()

	if err != nil {
		t.errC <- err
	}
	t.errC = nil
}

func (t *Torrent) close() {
	t.stop(errors.New("torrent is closed"))

	for _, oh := range t.outgoingHandshakers {
		oh.Close()
	}
	for _, ih := range t.incomingHandshakers {
		ih.Close()
	}
	for _, id := range t.infoDownloads {
		id.Close()
	}
	for _, pd := range t.pieceDownloads {
		pd.Close()
	}
	for _, ip := range t.incomingPeers {
		ip.Close()
	}
	for _, op := range t.outgoingPeers {
		op.Close()
	}
	// TODO close data
	// TODO order closes here
	close(t.closedC)
}

func (t *Torrent) run() {

	// TODO where to put this? this may take long
	if t.info != nil {
		err2 := t.processInfo()
		if err2 != nil {
			t.log.Errorln("cannot process info:", err2)
			t.errC <- err2
			return
		}
	}

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
		case addrs := <-t.addrsFromTrackers:
			t.addrList.Push(addrs, t.port)
			t.dialLimit.Signal(len(addrs))
		case req := <-t.statsCommandC:
			var stats Stats
			if t.info != nil {
				stats.BytesTotal = t.info.TotalLength
				// TODO this is wrong, pre-calculate complete and incomplete bytes
				stats.BytesComplete = int64(t.info.PieceLength) * int64(t.bitfield.Count())
				stats.BytesIncomplete = stats.BytesTotal - stats.BytesComplete
				// TODO calculate bytes downloaded
				// TODO calculate bytes uploaded
			} else {
				stats.BytesIncomplete = math.MaxUint32
				// TODO this is wrong, pre-calculate complete and incomplete bytes
			}
			req.Response <- stats
		case <-t.dialLimit.Ready:
			addr := t.addrList.Pop()
			if addr == nil {
				t.dialLimit.Stop()
				break
			}
			h := outgoinghandshaker.NewOutgoing(addr, t.peerID, t.infoHash, t.outgoingHandshakerResultC, t.log)
			t.outgoingHandshakers[addr.String()] = h
			go h.Run()
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
			if t.info == nil {
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
			// TODO handle info downloader result
			t.connectedPeers[res.Peer].InfoDownloader = nil
			delete(t.infoDownloads, res.Peer)
			t.infoDownloaders.Signal(1)
			if res.Error != nil {
				res.Peer.Logger().Error(res.Error)
				res.Peer.Close()
				break
			}
			hash := sha1.New()    // nolint: gosec
			hash.Write(res.Bytes) // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), t.infoHash[:]) {
				res.Peer.Logger().Errorln("received info does not match with hash")
				t.infoDownloaders.Signal(1)
				res.Peer.Close()
				break
			}
			var err error
			t.info, err = metainfo.NewInfo(res.Bytes)
			if err != nil {
				t.log.Errorln("cannot parse info bytes:", err)
				t.errC <- err
				return
			}
			if t.resume != nil {
				err = t.resume.WriteInfo(t.info.Bytes)
				if err != nil {
					t.log.Errorln("cannot write resume info:", err)
					t.errC <- err
					return
				}
			}
			err = t.processInfo()
			if err != nil {
				t.log.Errorln("cannot process info:", err)
				t.errC <- err
				return
			}
			// process previously received messages
			for _, pe := range t.connectedPeers {
				for _, msg := range pe.Messages {
					pm := peer.Message{Peer: pe, Message: msg}
					t.handlePeerMessage(pm)
				}
			}
			t.infoDownloaders.Stop()
			t.pieceDownloaders.Signal(parallelPieceDownloads)
		case <-t.pieceDownloaders.Ready:
			if t.info == nil {
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
			// TODO fix nil pointer exception
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
			// TODO skip peers already having that piece
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
			t.startPeer(res.Peer, &t.incomingPeers)
		case res := <-t.outgoingHandshakerResultC:
			delete(t.outgoingHandshakers, res.Addr.String())
			if res.Error != nil {
				break
			}
			t.startPeer(res.Peer, &t.outgoingPeers)
		case pe := <-t.peerDisconnectedC:
			delete(t.peerIDs, pe.ID())
			if pe.Downloader != nil {
				pe.Downloader.Close()
			}
			delete(t.connectedPeers, pe.Peer)
			for i := range t.pieces {
				delete(t.pieces[i].HavingPeers, pe.Peer)
				delete(t.pieces[i].AllowedFastPeers, pe.Peer)
				delete(t.pieces[i].RequestedPeers, pe.Peer)
			}
		case pm := <-t.messages:
			t.handlePeerMessage(pm)
		}
	}
}

func (t *Torrent) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		// Save have messages for processesing later received while we don't have info yet.
		if t.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Peer.Logger().Errorln("unexpected piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		pi := &t.data.Pieces[msg.Index]
		pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has piece #", pi.Index)
		t.pieceDownloaders.Signal(1)
		t.pieces[pi.Index].HavingPeers[pe.Peer] = struct{}{}
		t.updateInterestedState(pe)
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if t.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		numBytes := uint32(bitfield.NumBytes(uint32(len(t.data.Pieces))))
		if uint32(len(msg.Data)) != numBytes {
			pe.Peer.Logger().Errorln("invalid bitfield length:", len(msg.Data))
			pe.Peer.Close()
			break
		}
		bf := bitfield.NewBytes(msg.Data, uint32(len(t.data.Pieces)))
		pe.Peer.Logger().Debugln("Received bitfield:", bf.Hex())
		for i := uint32(0); i < bf.Len(); i++ {
			if bf.Test(i) {
				t.pieces[i].HavingPeers[pe.Peer] = struct{}{}
			}
		}
		t.pieceDownloaders.Signal(int(bf.Count()))
		t.updateInterestedState(pe)
	case peerprotocol.HaveAllMessage:
		if t.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		for i := range t.pieces {
			t.pieces[i].HavingPeers[pe.Peer] = struct{}{}
		}
		t.pieceDownloaders.Signal(len(t.pieces))
		t.updateInterestedState(pe)
	case peerprotocol.HaveNoneMessage:
		// TODO handle?
	case peerprotocol.AllowedFastMessage:
		if t.info == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		pi := &t.data.Pieces[msg.Index]
		pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has allowed fast for piece #", pi.Index)
		t.pieces[msg.Index].AllowedFastPeers[pe.Peer] = struct{}{}
	case peerprotocol.UnchokeMessage:
		t.pieceDownloaders.Signal(1)
		pe.PeerChoking = false
		if pd, ok := t.pieceDownloads[pe.Peer]; ok {
			pd.UnchokeC <- struct{}{}
		}
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		if pd, ok := t.pieceDownloads[pe.Peer]; ok {
			pd.ChokeC <- struct{}{}
		}
	case peerprotocol.InterestedMessage:
		// TODO handle intereseted messages
		_ = pe
	case peerprotocol.NotInterestedMessage:
		// TODO handle not intereseted messages
		_ = pe
	case peerprotocol.PieceMessage:
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid piece index:", msg.Index)
			pe.Peer.Close()
			break
		}
		piece := &t.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Peer.Logger().Errorln("invalid piece begin:", msg.Begin, "length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pe.BytesDownlaodedInChokePeriod += int64(len(msg.Data))
		if pd, ok := t.pieceDownloads[pe.Peer]; ok {
			pd.PieceC <- piecedownloader.Piece{Block: block, Data: msg.Data}
		}
	case peerprotocol.RequestMessage:
		if t.info == nil {
			pe.Peer.Logger().Error("request received but we don't have info")
			pe.Peer.Close()
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid request index:", msg.Index)
			pe.Peer.Close()
			break
		}
		if msg.Begin+msg.Length > t.data.Pieces[msg.Index].Length {
			pe.Peer.Logger().Errorln("invalid request length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pi := &t.data.Pieces[msg.Index]
		if pe.AmChoking {
			if pe.Peer.FastExtension {
				m := peerprotocol.RejectMessage{RequestMessage: msg}
				pe.SendMessage(m)
			}
		} else {
			pe.Peer.SendPiece(msg, pi)
		}
	case peerprotocol.RejectMessage:
		if t.info == nil {
			pe.Peer.Logger().Error("reject received but we don't have info")
			pe.Peer.Close()
			break
		}

		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Peer.Logger().Errorln("invalid reject index:", msg.Index)
			pe.Peer.Close()
			break
		}
		piece := &t.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Peer.Logger().Errorln("invalid reject begin:", msg.Begin, "length:", msg.Length)
			pe.Peer.Close()
			break
		}
		pd, ok := t.pieceDownloads[pe.Peer]
		if !ok {
			pe.Peer.Logger().Error("reject received but we don't have active download")
			pe.Peer.Close()
			break
		}
		pd.RejectC <- block
	// TODO make it value type
	case *peerprotocol.ExtensionHandshakeMessage:
		t.log.Debugln("extension handshake received", msg)
		pe.ExtensionHandshake = msg
		t.infoDownloaders.Signal(1)
	// TODO make it value type
	case *peerprotocol.ExtensionMetadataMessage:
		switch msg.Type {
		case peerprotocol.ExtensionMetadataMessageTypeRequest:
			if t.info == nil {
				// TODO send reject
				break
			}
			extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
			if !ok {
				// TODO send reject
			}
			// TODO Clients MAY implement flood protection by rejecting request messages after a certain number of them have been served. Typically the number of pieces of metadata times a factor.
			start := 16 * 1024 * msg.Piece
			end := 16 * 1024 * (msg.Piece + 1)
			totalSize := uint32(len(t.info.Bytes))
			if end > totalSize {
				end = totalSize
			}
			data := t.info.Bytes[start:end]
			dataMsg := peerprotocol.ExtensionMetadataMessage{
				Type:      peerprotocol.ExtensionMetadataMessageTypeData,
				Piece:     msg.Piece,
				TotalSize: totalSize,
				Data:      data,
			}
			extDataMsg := peerprotocol.ExtensionMessage{
				ExtendedMessageID: extMsgID,
				Payload:           &dataMsg,
			}
			pe.Peer.SendMessage(extDataMsg)
		case peerprotocol.ExtensionMetadataMessageTypeData:
			id, ok := t.infoDownloads[pe.Peer]
			if !ok {
				pe.Peer.Logger().Warningln("received unexpected metadata piece:", msg.Piece)
				break
			}
			select {
			case id.DataC <- infodownloader.Data{Index: msg.Piece, Data: msg.Data}:
			case <-t.closeC:
				return
			}
		case peerprotocol.ExtensionMetadataMessageTypeReject:
			// TODO handle metadata piece reject
		}
	default:
		panic(fmt.Sprintf("unhandled peer message type: %T", msg))
	}
}

func (t *Torrent) startPeer(p *ip.Peer, peers *[]*peer.Peer) {
	_, ok := t.peerIDs[p.ID()]
	if ok {
		p.Logger().Errorln("peer with same id already connected:", p.ID())
		p.Close()
		return
	}
	t.peerIDs[p.ID()] = struct{}{}

	pe := peer.New(p, t.messages, t.peerDisconnectedC)
	t.connectedPeers[p] = pe
	*peers = append(*peers, pe) // TODO remove from this list
	go pe.Run()

	t.sendFirstMessage(p)
	if len(t.connectedPeers) <= 4 {
		t.unchokePeer(pe)
	}
}

func (t *Torrent) sendFirstMessage(p *ip.Peer) {
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

func (t *Torrent) processInfo() error {
	var err error
	t.data, err = torrentdata.New(t.info, t.storage)
	if err != nil {
		return err
	}
	// TODO defer data.Close()

	if t.bitfield == nil {
		t.bitfield = bitfield.New(t.info.NumPieces)
		if t.data.Exists {
			buf := make([]byte, t.info.PieceLength)
			hash := sha1.New() // nolint: gosec
			for _, p := range t.data.Pieces {
				buf = buf[:p.Length]
				_, err = p.Data.ReadAt(buf, 0)
				if err != nil {
					return err
				}
				ok := p.VerifyHash(buf[:p.Length], hash)
				t.bitfield.SetTo(p.Index, ok)
				hash.Reset()
			}
			if t.resume != nil {
				err = t.resume.WriteBitfield(t.bitfield.Bytes())
				if err != nil {
					return err
				}
			}
		}
	}
	t.checkCompletion()
	t.preparePieces()
	return nil
}

func (t *Torrent) preparePieces() {
	pieces := make([]piece.Piece, len(t.data.Pieces))
	sortedPieces := make([]*piece.Piece, len(t.data.Pieces))
	for i := range t.data.Pieces {
		pieces[i] = piece.New(&t.data.Pieces[i])
		sortedPieces[i] = &pieces[i]
	}
	t.pieces = pieces
	t.sortedPieces = sortedPieces
}

func (t *Torrent) nextInfoDownload() *infodownloader.InfoDownloader {
	for _, pe := range t.connectedPeers {
		if pe.InfoDownloader != nil {
			continue
		}
		extID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe.Peer, extID, pe.ExtensionHandshake.MetadataSize, t.infoDownloaderResultC)
	}
	return nil
}

func (t *Torrent) nextPieceDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(piece.ByAvailability(t.sortedPieces))
	for _, p := range t.sortedPieces {
		if t.bitfield.Test(p.Index) {
			continue
		}
		if len(p.RequestedPeers) > 0 {
			continue
		}
		if p.Writing {
			continue
		}
		if len(p.HavingPeers) == 0 {
			continue
		}
		// prefer allowed fast peers first
		for pe := range p.HavingPeers {
			if _, ok := p.AllowedFastPeers[pe]; !ok {
				continue
			}
			if _, ok := t.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, t.pieceDownloaderResultC)
		}
		for pe := range p.HavingPeers {
			if pp, ok := t.connectedPeers[pe]; ok && pp.PeerChoking {
				continue
			}
			if _, ok := t.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, t.pieceDownloaderResultC)
		}
	}
	return nil
}

func (t *Torrent) updateInterestedState(pe *peer.Peer) {
	if t.info == nil {
		return
	}
	interested := false
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		weHave := t.bitfield.Test(i)
		_, peerHave := t.pieces[i].HavingPeers[pe.Peer]
		if !weHave && peerHave {
			interested = true
			break
		}
	}
	if !pe.AmInterested && interested {
		pe.AmInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.Peer.SendMessage(msg)
		return
	}
	if pe.AmInterested && !interested {
		pe.AmInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.Peer.SendMessage(msg)
		return
	}
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
		close(t.completeC)
		t.completed = true
	}
}
