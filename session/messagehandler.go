package session

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"net"

	"github.com/cenkalti/rain/internal/addrlist"
	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/piecewriter"
	"github.com/cenkalti/rain/internal/tracker"
)

func (t *torrent) handlePieceMessage(pm peer.PieceMessage) {
	msg := pm.Piece
	pe := pm.Peer
	if t.pieces == nil || t.bitfield == nil {
		pe.Logger().Error("piece received but we don't have info")
		t.byteStats.BytesWasted += int64(len(msg.Data))
		t.closePeer(pe)
		return
	}
	if msg.Index >= uint32(len(t.pieces)) {
		pe.Logger().Errorln("invalid piece index:", msg.Index)
		t.byteStats.BytesWasted += int64(len(msg.Data))
		t.closePeer(pe)
		return
	}
	piece := &t.pieces[msg.Index]
	block := piece.Blocks.Find(msg.Begin, uint32(len(msg.Data)))
	if block == nil {
		pe.Logger().Errorln("invalid piece begin:", msg.Begin, "length:", len(msg.Data))
		t.byteStats.BytesWasted += int64(len(msg.Data))
		t.closePeer(pe)
		return
	}
	t.byteStats.BytesDownloaded += int64(len(msg.Data))
	pe.BytesDownlaodedInChokePeriod += int64(len(msg.Data))
	pd, ok := t.pieceDownloaders[pe]
	if !ok {
		t.byteStats.BytesWasted += int64(len(msg.Data))
		peerreader.PiecePool.Put(msg.Data)
		return
	}
	if pd.Piece.Index != msg.Index {
		t.byteStats.BytesWasted += int64(len(msg.Data))
		peerreader.PiecePool.Put(msg.Data)
		return
	}
	pd.GotBlock(block, msg.Data)
	peerreader.PiecePool.Put(msg.Data)
	if !pd.Done() {
		pd.RequestBlocks(t.config.RequestQueueLength)
		pe.ResetSnubTimer()
		return
	}
	// t.log.Debugln("piece download completed. index:", pd.Piece.Index)
	t.closePieceDownloader(pd)
	pe.StopSnubTimer()

	ok = piece.VerifyHash(pd.Buffer[:pd.Piece.Length], sha1.New()) // nolint: gosec
	if !ok {
		t.byteStats.BytesWasted += int64(len(msg.Data))
		// TODO ban peers that sent corrupt piece
		t.log.Error("received corrupt piece")
		t.closePeer(pd.Peer)
		t.startPieceDownloaders()
		return
	}

	if t.piecePicker != nil {
		for pe := range t.piecePicker.RequestedPeers(piece.Index) {
			pd2 := t.pieceDownloaders[pe]
			t.closePieceDownloader(pd2)
			pd2.CancelPending()
		}
	}

	if piece.Writing {
		panic("piece already writing")
	}
	piece.Writing = true

	t.blockPieceMessages = t.pieceMessages
	t.pieceMessages = nil

	pw := piecewriter.New(piece, pd.Buffer, pd.Piece.Length)
	go pw.Run(t.pieceWriterResultC)

	t.startPieceDownloaders()
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
		pi := &t.pieces[msg.Index]
		// pe.Logger().Debug("Peer ", pe.String(), " has piece #", pi.Index)
		if t.piecePicker != nil {
			t.piecePicker.HandleHave(pe, pi.Index)
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaders()
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if t.pieces == nil || t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		bf, err := bitfield.NewBytes(msg.Data, t.info.NumPieces)
		if err != nil {
			pe.Logger().Errorln(err)
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
		t.startPieceDownloaders()
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
		t.startPieceDownloaders()
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
		pi := &t.pieces[msg.Index]
		pe.Logger().Debug("Peer ", pe.String(), " has allowed fast for piece #", pi.Index)
		if t.piecePicker != nil {
			t.piecePicker.HandleAllowedFast(pe, msg.Index)
		}
	case peerprotocol.UnchokeMessage:
		pe.PeerChoking = false
		if pd, ok := t.pieceDownloaders[pe]; ok {
			pd.RequestBlocks(t.config.RequestQueueLength)
		}
		t.startPieceDownloaders()
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		if pd, ok := t.pieceDownloaders[pe]; ok {
			pd.Choked()
			t.pieceDownloadersChoked[pe] = pd
			t.startPieceDownloaders()
		}
	case peerprotocol.InterestedMessage:
		pe.PeerInterested = true
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
		if pe.AmChoking {
			if pe.FastExtension {
				m := peerprotocol.RejectMessage{RequestMessage: msg}
				pe.SendMessage(m)
			}
		} else {
			pe.SendPiece(msg, t.cachedPiece(pi))
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
		piece := &t.pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Logger().Errorln("invalid reject begin:", msg.Begin, "length:", msg.Length)
			t.closePeer(pe)
			break
		}
		pd, ok := t.pieceDownloaders[pe]
		if ok {
			pd.Rejected(block)
		}
	case peerprotocol.CancelMessage:
		if t.pieces == nil || t.bitfield == nil {
			pe.Logger().Error("cancel received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= t.info.NumPieces {
			pe.Logger().Errorln("invalid cancel index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pe.CancelRequest(msg)
	case peerwriter.BlockUploaded:
		t.byteStats.BytesUploaded += int64(msg.Length)
		pe.BytesUploadedInChokePeriod += int64(msg.Length)
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
		if t.config.PEXEnabled {
			if _, ok := msg.M[peerprotocol.ExtensionKeyPEX]; ok {
				if t.info != nil && t.info.Private != 1 {
					pe.StartPEX(t.peers)
				}
			}
		}
	case peerprotocol.ExtensionMetadataMessage:
		switch msg.Type {
		case peerprotocol.ExtensionMetadataMessageTypeRequest:
			extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
			if !ok {
				break
			}
			if t.info == nil {
				t.sendMetadataReject(pe, msg.Piece, extMsgID)
				break
			}
			// TODO Clients MAY implement flood protection by rejecting request messages after a certain number of them have been served. Typically the number of pieces of metadata times a factor.
			start := 16 * 1024 * msg.Piece
			end := start + 16*1024
			totalSize := uint32(len(t.info.Bytes))
			if end > totalSize {
				end = totalSize
			}
			if start >= totalSize {
				t.sendMetadataReject(pe, msg.Piece, extMsgID)
				break
			}
			if end > totalSize {
				t.sendMetadataReject(pe, msg.Piece, extMsgID)
				break
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
				Payload:           dataMsg,
			}
			pe.SendMessage(extDataMsg)
		case peerprotocol.ExtensionMetadataMessageTypeData:
			id, ok := t.infoDownloaders[pe]
			if !ok {
				break
			}
			err := id.GotBlock(msg.Piece, msg.Data)
			if err != nil {
				pe.Logger().Error(err)
				t.closePeer(pe)
				t.startInfoDownloaders()
				break
			}
			if !id.Done() {
				id.RequestBlocks(t.config.RequestQueueLength)
				pe.ResetSnubTimer()
				break
			}
			pe.StopSnubTimer()

			hash := sha1.New()                              // nolint: gosec
			hash.Write(id.Bytes)                            // nolint: gosec
			if !bytes.Equal(hash.Sum(nil), t.infoHash[:]) { // nolint: gosec
				pe.Logger().Errorln("received info does not match with hash")
				t.closePeer(id.Peer)
				t.startInfoDownloaders()
				break
			}
			t.stopInfoDownloaders()

			info, err := metainfo.NewInfo(id.Bytes)
			if err != nil {
				err = fmt.Errorf("cannot parse info bytes: %s", err)
				t.log.Error(err)
				t.stop(err)
				break
			}
			if info.Private == 1 {
				err = errors.New("private torrent from magnet")
				t.log.Error(err)
				t.stop(err)
				break
			}
			t.info = info
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
		case peerprotocol.ExtensionMetadataMessageTypeReject:
			id, ok := t.infoDownloaders[pe]
			if ok {
				t.closePeer(id.Peer)
				t.startInfoDownloaders()
			}
		}
	case peerprotocol.ExtensionPEXMessage:
		if !t.config.PEXEnabled {
			break
		}
		addrs, err := tracker.DecodePeersCompact([]byte(msg.Added))
		if err != nil {
			t.log.Error(err)
			break
		}
		t.handleNewPeers(addrs, addrlist.PEX)
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
			peerHave := t.piecePicker.DoesHave(pe, i)
			if !weHave && peerHave {
				interested = true
				break
			}
		}
	}
	if !pe.AmInterested && interested {
		pe.AmInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.SendMessage(msg)
		return
	}
	if pe.AmInterested && !interested {
		pe.AmInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.SendMessage(msg)
		return
	}
}

func (t *torrent) sendMetadataReject(pe *peer.Peer, i uint32, msgID uint8) {
	dataMsg := peerprotocol.ExtensionMetadataMessage{
		Type:  peerprotocol.ExtensionMetadataMessageTypeReject,
		Piece: i,
	}
	extDataMsg := peerprotocol.ExtensionMessage{
		ExtendedMessageID: msgID,
		Payload:           &dataMsg,
	}
	pe.SendMessage(extDataMsg)
}
