package torrent

import (
	"fmt"

	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/torrent/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/tracker"
)

func (t *Torrent) handlePeerMessage(pm peer.Message) {
	pe := pm.Peer
	switch msg := pm.Message.(type) {
	case peerprotocol.HaveMessage:
		// Save have messages for processesing later received while we don't have info yet.
		if t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Logger().Errorln("unexpected piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pi := &t.data.Pieces[msg.Index]
		pe.Logger().Debug("Peer ", pe.String(), " has piece #", pi.Index)
		t.pieces[pi.Index].HavingPeers[pe] = struct{}{}
		t.updateInterestedState(pe)
		t.startPieceDownloaders()
	case peerprotocol.BitfieldMessage:
		// Save bitfield messages while we don't have info yet.
		if t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		numBytes := uint32(bitfield.NumBytes(uint32(len(t.data.Pieces))))
		if uint32(len(msg.Data)) != numBytes {
			pe.Logger().Errorln("invalid bitfield length:", len(msg.Data))
			t.closePeer(pe)
			break
		}
		bf := bitfield.NewBytes(msg.Data, uint32(len(t.data.Pieces)))
		pe.Logger().Debugln("Received bitfield:", bf.Hex())
		for i := uint32(0); i < bf.Len(); i++ {
			if bf.Test(i) {
				t.pieces[i].HavingPeers[pe] = struct{}{}
			}
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaders()
	case peerprotocol.HaveAllMessage:
		if t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		for i := range t.pieces {
			t.pieces[i].HavingPeers[pe] = struct{}{}
		}
		t.updateInterestedState(pe)
		t.startPieceDownloaders()
	case peerprotocol.HaveNoneMessage: // TODO handle?
	case peerprotocol.AllowedFastMessage:
		if t.bitfield == nil {
			pe.Messages = append(pe.Messages, msg)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		pi := &t.data.Pieces[msg.Index]
		pe.Logger().Debug("Peer ", pe.String(), " has allowed fast for piece #", pi.Index)
		t.pieces[msg.Index].AllowedFastPeers[pe] = struct{}{}
		pe.AllowedFastPieces[msg.Index] = struct{}{}
	case peerprotocol.UnchokeMessage:
		pe.PeerChoking = false
		if pd, ok := t.pieceDownloaders[pe]; ok {
			pd.Unchoke()
		}
		t.startPieceDownloaders()
	case peerprotocol.ChokeMessage:
		pe.PeerChoking = true
		if pd, ok := t.pieceDownloaders[pe]; ok {
			pd.Choke()
			t.pieceDownloadersChoked[pe] = pd
			t.startPieceDownloaders()
		}
	case peerprotocol.InterestedMessage:
		// TODO handle intereseted messages
	case peerprotocol.NotInterestedMessage:
		// TODO handle not intereseted messages
	case peerreader.Piece:
		if t.bitfield == nil {
			pe.Logger().Error("piece received but we don't have info")
			t.closePeer(pe)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Logger().Errorln("invalid piece index:", msg.Index)
			t.closePeer(pe)
			break
		}
		piece := &t.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, uint32(len(msg.Data)))
		if block == nil {
			pe.Logger().Errorln("invalid piece begin:", msg.Begin, "length:", len(msg.Data))
			t.closePeer(pe)
			break
		}
		pe.BytesDownlaodedInChokePeriod += int64(len(msg.Data))
		t.bytesDownloaded += int64(len(msg.Data))
		if pd, ok := t.pieceDownloaders[pe]; ok && pd.Piece.Index == msg.Index {
			pd.Download(block, msg.Data)
		} else {
			t.bytesWasted += int64(len(msg.Data))
		}
	case peerprotocol.RequestMessage:
		if t.bitfield == nil {
			pe.Logger().Error("request received but we don't have info")
			t.closePeer(pe)
			break
		}
		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Logger().Errorln("invalid request index:", msg.Index)
			t.closePeer(pe)
			break
		}
		if msg.Begin+msg.Length > t.data.Pieces[msg.Index].Length {
			pe.Logger().Errorln("invalid request length:", msg.Length)
			t.closePeer(pe)
			break
		}
		pi := &t.data.Pieces[msg.Index]
		if pe.AmChoking {
			if pe.FastExtension {
				m := peerprotocol.RejectMessage{RequestMessage: msg}
				pe.SendMessage(m)
			}
		} else {
			pe.SendPiece(msg, pi.Data)
		}
	case peerprotocol.RejectMessage:
		if t.bitfield == nil {
			pe.Logger().Error("reject received but we don't have info")
			t.closePeer(pe)
			break
		}

		if msg.Index >= uint32(len(t.data.Pieces)) {
			pe.Logger().Errorln("invalid reject index:", msg.Index)
			t.closePeer(pe)
			break
		}
		piece := &t.data.Pieces[msg.Index]
		block := piece.Blocks.Find(msg.Begin, msg.Length)
		if block == nil {
			pe.Logger().Errorln("invalid reject begin:", msg.Begin, "length:", msg.Length)
			t.closePeer(pe)
			break
		}
		// TODO check piece index
		// TODO check piece index on cancel
		pd, ok := t.pieceDownloaders[pe]
		if !ok {
			pe.Logger().Error("reject received but we don't have active download")
			t.closePeer(pe)
			break
		}
		pd.Reject(block)
	case peerwriter.BlockUploaded:
		t.bytesUploaded += int64(msg.Length)
		pe.BytesUploadedInChokePeriod += int64(msg.Length)
	// TODO make it value type
	case *peerprotocol.ExtensionHandshakeMessage:
		pe.Logger().Debugln("extension handshake received:", msg)
		pe.ExtensionHandshake = msg
		t.startInfoDownloaders()
		if _, ok := msg.M[peerprotocol.ExtensionKeyPEX]; ok {
			pe.StartPEX(t.peers)
		}
	// TODO make it value type
	case *peerprotocol.ExtensionMetadataMessage:
		switch msg.Type {
		case peerprotocol.ExtensionMetadataMessageTypeRequest:
			if t.info == nil {
				// TODO send reject
				break
			}
			extMsgID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
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
			pe.SendMessage(extDataMsg)
		case peerprotocol.ExtensionMetadataMessageTypeData:
			id, ok := t.infoDownloaders[pe]
			if ok {
				id.Download(msg.Piece, msg.Data)
			}
		case peerprotocol.ExtensionMetadataMessageTypeReject:
			id, ok := t.infoDownloaders[pe]
			if ok {
				t.closePeer(id.Peer)
				t.startInfoDownloaders()
			}
		}
	case *peerprotocol.ExtensionPEXMessage:
		addrs, err := tracker.DecodePeersCompact([]byte(msg.Added))
		if err != nil {
			t.log.Error(err)
			break
		}
		t.handleNewPeers(addrs, "pex")
	default:
		panic(fmt.Sprintf("unhandled peer message type: %T", msg))
	}
}

func (t *Torrent) updateInterestedState(pe *peer.Peer) {
	if t.info == nil {
		return
	}
	interested := false
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		weHave := t.bitfield.Test(i)
		_, peerHave := t.pieces[i].HavingPeers[pe]
		if !weHave && peerHave {
			interested = true
			break
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
