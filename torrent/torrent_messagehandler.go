package torrent

import (
	"fmt"
	"net"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/cachedpiece"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/peersource"
	"github.com/cenkalti/rain/internal/piecedownloader"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/tracker"
)

func (t *torrent) handlePieceMessage(pm peer.PieceMessage) {
	msg := pm.Piece
	pe := pm.Peer
	l := int64(len(msg.Buffer.Data))
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("piece received but we don't have info")
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	if msg.Index >= uint32(len(t.pieces)) {
		pe.Logger().Errorln("invalid piece index:", msg.Index)
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	t.downloadSpeed.Mark(l)
	t.bytesDownloaded.Inc(l)
	t.session.metrics.SpeedDownload.Mark(l)
	pd, ok := t.pieceDownloaders[pe]
	if !ok {
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return
	}
	if pd.Piece.Index != msg.Index {
		t.bytesWasted.Inc(l)
		msg.Buffer.Release()
		return
	}
	piece := pd.Piece
	block, ok := piece.FindBlock(msg.Begin, uint32(len(msg.Buffer.Data)))
	if !ok {
		pe.Logger().Errorln("invalid piece index:", msg.Index, "begin:", msg.Begin, "length:", len(msg.Buffer.Data))
		t.bytesWasted.Inc(l)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	err := pd.GotBlock(block, msg.Buffer.Data)
	switch err {
	case piecedownloader.ErrBlockDuplicate:
		if pe.FastEnabled {
			pe.Logger().Warningln("received duplicate block:", block.Index)
		} else {
			// If peer does not support fast extension, we cancel all pending requests on choke message.
			// After an unchoke we request them again. Some clients appears to be sending the same block
			// if we request it twice.
			pe.Logger().Debugln("received duplicate block:", block.Index)
		}
	case piecedownloader.ErrBlockNotRequested:
		if pe.FastEnabled {
			pe.Logger().Warningln("received not requested block:", block.Index)
		} else {
			// If peer does not support fast extension, we cancel all pending requests on choke message.
			// That's why we think that we have received an unrequested block.
			pe.Logger().Debugln("received not requested block:", block.Index)
		}
	case nil:
	default:
		pe.Logger().Error(err)
		t.closePeer(pe)
		msg.Buffer.Release()
		return
	}
	msg.Buffer.Release()
	if !pd.Done() {
		pe := pd.Peer.(*peer.Peer)
		if pd.AllowedFast || !pe.PeerChoking {
			pd.RequestBlocks(t.maxAllowedRequests(pe))
			pe.ResetSnubTimer()
		}
		return
	}
	t.log.Debugf("piece #%d downloaded from %s", msg.Index, pe.IP())
	t.closePieceDownloader(pd)
	pe.StopSnubTimer()

	if piece.Writing {
		panic("piece is already writing")
	}
	piece.Writing = true

	// Request next piece while writing the completed piece, being optimistic about hash check.
	t.startPieceDownloaderFor(pe)

	// Prevent receiving piece messages to avoid more than 1 write per torrent.
	t.pieceMessagesC.Suspend()
	t.webseedPieceResultC.Suspend()

	pw := piecewriter.New(piece, pe, pd.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)
}

func (t *torrent) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		// Save have messages for processesing later received while we don't have info yet.
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= t.info.NumPieces {
			pe.Logger().Errorln("unexpected piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		// pe.Logger().Debug("Peer ", pe.String(), " has piece #", pi.Index)
		if t.piecePicker != nil {
			t.piecePicker.HandleHave(pe, msg.Index)
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaderFor(pe)
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if len(msg.Data) == 0 {
			pe.Logger().Debugln("received bitfield length of zero")
			break
		}
		bf, err := bitfield.NewBytes(msg.Data, t.info.NumPieces)
		if err != nil {
			pe.Logger().Errorf("%s [len(bitfield)=%d] [numPieces=%d]", err, len(msg.Data), t.info.NumPieces)
			t.closePeer(pe)
			break
		}
		pe.Logger().Debugln("Received bitfield:", bf.Hex())
		if t.piecePicker != nil {
			for i := uint32(0); i < bf.Len(); i++ {
				if bf.Test(i) {
					t.piecePicker.HandleHave(pe, i)
				}
			}
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaderFor(pe)
	case peerprotocol.HaveAllMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if t.piecePicker != nil {
			for _, pi := range t.pieces {
				t.piecePicker.HandleHave(pe, pi.Index)
			}
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaderFor(pe)
	case peerprotocol.HaveNoneMessage:
	case peerprotocol.AllowedFastMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= t.info.NumPieces {
			pe.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pe.Logger().Debug("Peer ", pe.String(), " has allowed fast for piece #", msg.Index)
		if t.piecePicker != nil {
			t.piecePicker.HandleAllowedFast(pe, msg.Index)
		}
	case peerprotocol.UnchokeMessage:
		pe.PeerChoking = false
		pd, ok := t.pieceDownloaders[pe]
		if !ok {
			t.startPieceDownloaderFor(pe)
			break
		}
		if pd.AllowedFast {
			break
		}
		delete(t.pieceDownloadersChoked, pd.Peer.(*peer.Peer))
		pd.RequestBlocks(t.maxAllowedRequests(pe))
		pe.ResetSnubTimer()
		if t.piecePicker != nil {
			t.piecePicker.HandleUnchoke(pe, pd.Piece.Index)
		}
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		pd, ok := t.pieceDownloaders[pe]
		if !ok {
			break
		}
		if pd.AllowedFast {
			break
		}
		pd.Choked()
		pe.StopSnubTimer()
		t.pieceDownloadersChoked[pe] = pd
		delete(t.pieceDownloadersSnubbed, pe)
		if t.piecePicker != nil {
			t.piecePicker.HandleChoke(pe, pd.Piece.Index)
		}
		t.startPieceDownloaders()
	case peerprotocol.InterestedMessage:
		pe.PeerInterested = true
		t.unchoker.FastUnchoke(pe)
	case peerprotocol.NotInterestedMessage:
		pe.PeerInterested = false
	case peerprotocol.RequestMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("request received but we don't have info")
			t.closePeer(pe)
			break
		}
		if msg.Index >= t.info.NumPieces {
			pe.Logger().Errorln("invalid request index:", msg.Index)
			t.closePeer(pe)
			break
		}
		if msg.Begin+msg.Length > t.pieces[msg.Index].Length {
			pe.Logger().Errorln("invalid request length:", msg.Length)
			t.closePeer(pe)
			break
		}
		pi := &t.pieces[msg.Index]
		if !pi.Done {
			m := peerprotocol.RejectMessage{RequestMessage: msg}
			pe.SendMessage(m)
			break
		}
		if pe.ClientChoking {
			if pe.FastEnabled {
				if pe.SentAllowedFast.Has(pi) {
					pe.SendPiece(msg, cachedpiece.New(pi, t.session.pieceCache, t.session.config.ReadCacheBlockSize, t.peerID))
				} else {
					m := peerprotocol.RejectMessage{RequestMessage: msg}
					pe.SendMessage(m)
				}
			}
		} else {
			pe.SendPiece(msg, cachedpiece.New(pi, t.session.pieceCache, t.session.config.ReadCacheBlockSize, t.peerID))
		}
	case peerprotocol.RejectMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("reject received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= t.info.NumPieces {
			pe.Logger().Errorln("invalid reject index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pd, ok := t.pieceDownloaders[pe]
		if !ok {
			break
		}
		if pd.Piece.Index != msg.Index {
			break
		}
		block, ok := pd.Piece.FindBlock(msg.Begin, msg.Length)
		if !ok {
			pe.Logger().Errorln("invalid reject index:", msg.Index, "begin:", msg.Begin, "length:", msg.Length)
			t.closePeer(pe)
			break
		}
		pd.Rejected(block)
	case peerprotocol.CancelMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("cancel received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= t.info.NumPieces {
			pe.Logger().Debugln("invalid cancel index:", msg.Index)
			break
		}
		pe.CancelRequest(msg)
		if pe.FastEnabled {
			pe.SendMessage(peerprotocol.RejectMessage{RequestMessage: peerprotocol.RequestMessage{
				Index:  msg.Index,
				Begin:  msg.Begin,
				Length: msg.Length,
			}})
		}
	case peerprotocol.PortMessage:
		if t.session.dht != nil {
			t.session.dht.AddNode(fmt.Sprintf("%s:%d", pe.IP(), msg.Port))
		}
	case peerwriter.BlockUploaded:
		l := int64(msg.Length)
		t.uploadSpeed.Mark(l)
		t.bytesUploaded.Inc(l)
		t.session.metrics.SpeedUpload.Mark(l)
	case peerprotocol.ExtensionHandshakeMessage:
		pe.Logger().Debugln("extension handshake received:", msg)
		if pe.ExtensionHandshake != nil {
			pe.Logger().Debugln("peer changed extensions")
			break
		}
		pe.ExtensionHandshake = &msg

		if len(msg.YourIP) == 4 {
			t.externalIP = net.IP(msg.YourIP)
		}
		if _, ok := msg.M[peerprotocol.ExtensionKeyMetadata]; ok {
			t.startInfoDownloaders()
		}
		if t.session.config.PEXEnabled {
			if _, ok := msg.M[peerprotocol.ExtensionKeyPEX]; ok {
				if t.info != nil && !t.info.Private {
					pe.StartPEX(t.peers, &t.recentlySeen)
				}
			}
		}
	case peerprotocol.ExtensionMetadataMessage:
		t.handleMetadataMessage(pe, msg)
	case peerprotocol.ExtensionPEXMessage:
		if !t.session.config.PEXEnabled {
			break
		}
		addrs, err := tracker.DecodePeersCompact([]byte(msg.Added))
		if err != nil {
			t.log.Error(err)
			break
		}
		t.handleNewPeers(addrs, peersource.PEX)
		addrs, err = tracker.DecodePeersCompact([]byte(msg.Dropped))
		if err != nil {
			t.log.Error(err)
			break
		}
		t.handleNewPeers(addrs, peersource.PEX)
	default:
		panic(fmt.Sprintf("unhandled peer message type: %T", msg))
	}
}

func (t *torrent) updateInterestedState(pe *peer.Peer) {
	if t.pieces == nil || t.bitfield == nil {
		return
	}
	interested := false
	if !t.completed {
		for i := uint32(0); i < t.bitfield.Len(); i++ {
			weHave := t.bitfield.Test(i)
			peerHave := pe.Bitfield.Test(i)
			if !weHave && peerHave {
				interested = true
				break
			}
		}
	}
	if !pe.ClientInterested && interested {
		pe.ClientInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.SendMessage(msg)
		return
	}
	if pe.ClientInterested && !interested {
		pe.ClientInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.SendMessage(msg)
		return
	}
}
