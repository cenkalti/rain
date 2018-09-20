package downloader

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/resume"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/worker"
)

const (
	parallelInfoDownloads  = 4
	parallelPieceDownloads = 4
	parallelPieceWrites    = 4
	parallelPieceReads     = 4
)

type PeerMessage struct {
	*peer.Peer
	Message interface{}
}

type Downloader struct {
	infoHash               [20]byte
	dest                   string
	resume                 resume.ResumeInfo
	info                   *metainfo.Info
	bitfield               *bitfield.Bitfield
	data                   *torrentdata.Data
	completed              bool
	pieces                 []Piece
	sortedPieces           []*Piece
	newPeers               <-chan *peer.Peer
	disconnectedPeers      chan *peer.Peer
	messages               chan PeerMessage
	connectedPeers         map[*peer.Peer]*Peer
	pieceDownloads         map[*peer.Peer]*piecedownloader.PieceDownloader
	infoDownloads          map[*peer.Peer]*infodownloader.InfoDownloader
	downloadDoneC          chan *piecedownloader.PieceDownloader
	infoDownloadDoneC      chan *infodownloader.InfoDownloader
	writeRequestC          chan piecewriter.Request
	writeResponseC         chan piecewriter.Response
	optimisticUnchokedPeer *Peer
	completeC              chan struct{}
	errC                   chan error
	log                    logger.Logger
	workers                worker.Workers
}

func New(infoHash [20]byte, dest string, res resume.ResumeInfo, info *metainfo.Info, bf *bitfield.Bitfield, newPeers <-chan *peer.Peer, completeC chan struct{}, errC chan error, l logger.Logger) *Downloader {
	return &Downloader{
		infoHash:          infoHash,
		dest:              dest,
		resume:            res,
		info:              info,
		bitfield:          bf,
		newPeers:          newPeers,
		disconnectedPeers: make(chan *peer.Peer),
		messages:          make(chan PeerMessage),
		connectedPeers:    make(map[*peer.Peer]*Peer),
		pieceDownloads:    make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		infoDownloads:     make(map[*peer.Peer]*infodownloader.InfoDownloader),
		downloadDoneC:     make(chan *piecedownloader.PieceDownloader),
		infoDownloadDoneC: make(chan *infodownloader.InfoDownloader),
		writeRequestC:     make(chan piecewriter.Request, 1),
		writeResponseC:    make(chan piecewriter.Response),
		completeC:         completeC,
		errC:              errC,
		log:               l,
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	if d.info != nil {
		err := d.processInfo()
		if err != nil {
			d.errC <- err
			return
		}
	}

	defer d.workers.Stop()
	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequestC, d.writeResponseC, d.log)
		d.workers.Start(w)
	}

	unchokeTimer := time.NewTicker(10 * time.Second)
	defer unchokeTimer.Stop()

	optimisticUnchokeTimer := time.NewTicker(30 * time.Second)
	defer optimisticUnchokeTimer.Stop()

	pieceDownloaders := semaphore.New(parallelPieceDownloads)
	infoDownloaders := semaphore.New(parallelInfoDownloads)

	for {
		select {
		case <-infoDownloaders.Wait:
			if d.info != nil {
				infoDownloaders.Block()
				break
			}
			id := d.nextInfoDownload()
			if id == nil {
				infoDownloaders.Block()
				break
			}
			d.log.Debugln("downloading info from", id.Peer.String())
			d.infoDownloads[id.Peer] = id
			d.connectedPeers[id.Peer].infoDownloader = id
			d.workers.StartWithOnFinishHandler(id, func() {
				select {
				case d.infoDownloadDoneC <- id:
				case <-stopC:
					return
				}
			})
		case id := <-d.infoDownloadDoneC:
			d.connectedPeers[id.Peer].infoDownloader = nil
			delete(d.infoDownloads, id.Peer)
			select {
			case buf := <-id.DoneC:
				hash := sha1.New() // nolint: gosec
				hash.Write(buf)    // nolint: gosec
				if !bytes.Equal(hash.Sum(nil), d.infoHash[:]) {
					id.Peer.Logger().Errorln("received info does not match with hash")
					infoDownloaders.Signal(1)
					id.Peer.Close()
					break
				}
				var err error
				d.info, err = metainfo.NewInfo(buf)
				if err != nil {
					d.log.Errorln("cannot parse info bytes:", err)
					d.errC <- err
					return
				}
				err = d.resume.WriteInfo(d.info.Bytes)
				if err != nil {
					d.log.Errorln("cannot write resume info:", err)
					d.errC <- err
					return
				}
				err = d.processInfo()
				if err != nil {
					d.log.Errorln("cannot process info:", err)
					d.errC <- err
					return
				}
				// process previously received messages
				for _, pe := range d.connectedPeers {
					go d.resendMessages(pe, stopC)
				}
				infoDownloaders.Block()
				pieceDownloaders.Signal(parallelPieceDownloads)
			}
		// 	// TODO handle error
		case <-pieceDownloaders.Wait:
			if d.info == nil {
				pieceDownloaders.Block()
				break
			}
			// TODO check status of existing downloads
			pd := d.nextDownload()
			if pd == nil {
				pieceDownloaders.Block()
				break
			}
			d.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
			d.pieceDownloads[pd.Peer] = pd
			d.pieces[pd.Piece.Index].requestedPeers[pd.Peer] = pd
			d.connectedPeers[pd.Peer].downloader = pd
			d.workers.StartWithOnFinishHandler(pd, func() {
				select {
				case d.downloadDoneC <- pd:
				case <-stopC:
					return
				}
			})
		case pd := <-d.downloadDoneC:
			d.log.Debugln("piece download completed. index:", pd.Piece.Index)
			d.connectedPeers[pd.Peer].downloader = nil
			delete(d.pieceDownloads, pd.Peer)
			delete(d.pieces[pd.Piece.Index].requestedPeers, pd.Peer)
			pieceDownloaders.Signal(1)
			select {
			case buf := <-pd.DoneC:
				ok := d.pieces[pd.Piece.Index].Piece.Verify(buf)
				if !ok {
					// TODO handle corrupt piece
					break
				}
				select {
				case d.writeRequestC <- piecewriter.Request{Piece: pd.Piece, Data: buf}:
					d.pieces[pd.Piece.Index].writing = true
				case <-stopC:
					return
				}
			case err := <-pd.ErrC:
				d.log.Errorln("could not download piece:", err)
				// TODO handle piece download error
			case <-stopC:
				return
			}
		case resp := <-d.writeResponseC:
			d.pieces[resp.Request.Piece.Index].writing = false
			if resp.Error != nil {
				d.errC <- resp.Error
				return
			}
			d.bitfield.Set(resp.Request.Piece.Index)
			println("XXX bitfield", d.bitfield.Hex(), len(d.bitfield.Bytes()), d.bitfield.Len())
			err := d.resume.WriteBitfield(d.bitfield.Bytes())
			if err != nil {
				d.log.Errorln("cannot write bitfield to resume db:", err)
				d.errC <- err
				return
			}
			d.checkCompletion()
			// TODO set bitfiled in resume data
			// d.resumeFile.bitfield[resp.Request.Piece.Index] = true
			// Tell everyone that we have this piece
			// TODO skip peers already having that piece
			for _, pe := range d.connectedPeers {
				msg := peerprotocol.HaveMessage{Index: resp.Request.Piece.Index}
				pe.SendMessage(msg, stopC)
				d.updateInterestedState(pe, stopC)
			}
		case <-unchokeTimer.C:
			peers := make([]*Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.optimisticUnhoked {
					peers = append(peers, pe)
				}
			}
			sort.Sort(ByDownloadRate(peers))
			for _, pe := range d.connectedPeers {
				pe.bytesDownlaodedInChokePeriod = 0
			}
			unchokedPeers := make(map[*Peer]struct{}, 3)
			for i, pe := range peers {
				if i == 3 {
					break
				}
				d.unchokePeer(pe, stopC)
				unchokedPeers[pe] = struct{}{}
			}
			for _, pe := range d.connectedPeers {
				if _, ok := unchokedPeers[pe]; !ok {
					d.chokePeer(pe, stopC)
				}
			}
		case <-optimisticUnchokeTimer.C:
			peers := make([]*Peer, 0, len(d.connectedPeers))
			for _, pe := range d.connectedPeers {
				if !pe.optimisticUnhoked && pe.amChoking {
					peers = append(peers, pe)
				}
			}
			if d.optimisticUnchokedPeer != nil {
				d.optimisticUnchokedPeer.optimisticUnhoked = false
				d.chokePeer(d.optimisticUnchokedPeer, stopC)
			}
			pe := peers[rand.Intn(len(peers))]
			pe.optimisticUnhoked = true
			d.unchokePeer(pe, stopC)
			d.optimisticUnchokedPeer = pe
		case p := <-d.newPeers:
			pe := NewPeer(p)
			d.connectedPeers[p] = pe
			bf := d.bitfield
			if p.FastExtension && bf != nil && bf.All() {
				msg := peerprotocol.HaveAllMessage{}
				p.SendMessage(msg, stopC)
			} else if p.FastExtension && (bf == nil || bf != nil && bf.Count() == 0) {
				msg := peerprotocol.HaveNoneMessage{}
				p.SendMessage(msg, stopC)
			} else if bf != nil {
				bitfieldData := make([]byte, len(bf.Bytes()))
				copy(bitfieldData, bf.Bytes())
				msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
				p.SendMessage(msg, stopC)
			}
			extHandshakeMsg := peerprotocol.NewExtensionHandshake()
			if d.info != nil {
				extHandshakeMsg.MetadataSize = d.info.InfoSize
			}
			msg := peerprotocol.ExtensionMessage{
				ExtendedMessageID: peerprotocol.ExtensionHandshakeID,
				Payload:           extHandshakeMsg,
			}
			p.SendMessage(msg, stopC)
			if len(d.connectedPeers) <= 4 {
				d.unchokePeer(pe, stopC)
			}
			go d.readMessages(pe.Peer)
		case p := <-d.disconnectedPeers:
			pe := d.connectedPeers[p]
			if pe.downloader != nil {
				pe.downloader.Close()
			}
			delete(d.connectedPeers, p)
			for i := range d.pieces {
				delete(d.pieces[i].havingPeers, p)
				delete(d.pieces[i].allowedFastPeers, p)
				delete(d.pieces[i].requestedPeers, p)
			}
		case <-stopC:
			return
		case pm := <-d.messages:
			pe := d.connectedPeers[pm.Peer]
			switch msg := pm.Message.(type) {
			case peerprotocol.HaveMessage:
				// Save have messages for processesing later received while we don't have info yet.
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("unexpected piece index:", msg.Index)
					pe.Peer.Close()
					break
				}
				pi := &d.data.Pieces[msg.Index]
				pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has piece #", pi.Index)
				pieceDownloaders.Signal(1)
				d.pieces[pi.Index].havingPeers[pe.Peer] = d.connectedPeers[pe.Peer]
				d.updateInterestedState(pe, stopC)
			case peerprotocol.BitfieldMessage:
				// Save bitfield messages while we don't have info yet.
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				numBytes := uint32(bitfield.NumBytes(uint32(len(d.data.Pieces))))
				if uint32(len(msg.Data)) != numBytes {
					pe.Peer.Logger().Errorln("invalid bitfield length:", len(msg.Data))
					pe.Peer.Close()
					break
				}
				bf := bitfield.NewBytes(msg.Data, uint32(len(d.data.Pieces)))
				pe.Peer.Logger().Debugln("Received bitfield:", bf.Hex())
				for i := uint32(0); i < bf.Len(); i++ {
					if bf.Test(i) {
						d.pieces[i].havingPeers[pe.Peer] = d.connectedPeers[pe.Peer]
					}
				}
				pieceDownloaders.Signal(bf.Count())
				d.updateInterestedState(pe, stopC)
			case peerprotocol.HaveAllMessage:
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				for i := range d.pieces {
					d.pieces[i].havingPeers[pe.Peer] = pe
				}
				pieceDownloaders.Signal(uint32(len(d.pieces)))
				d.updateInterestedState(pe, stopC)
			case peerprotocol.HaveNoneMessage:
				// TODO handle?
			case peerprotocol.AllowedFastMessage:
				if d.info == nil {
					pe.messages = append(pe.messages, msg)
					break
				}
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
					pe.Peer.Close()
					break
				}
				pi := &d.data.Pieces[msg.Index]
				pe.Peer.Logger().Debug("Peer ", pe.Peer.String(), " has allowed fast for piece #", pi.Index)
				d.pieces[msg.Index].allowedFastPeers[pe.Peer] = d.connectedPeers[pe.Peer]
			case peerprotocol.UnchokeMessage:
				pieceDownloaders.Signal(1)
				pe.peerChoking = false
				if pd, ok := d.pieceDownloads[pe.Peer]; ok {
					pd.UnchokeC <- struct{}{}
				}
			case peerprotocol.ChokeMessage:
				pe.peerChoking = true
				if pd, ok := d.pieceDownloads[pe.Peer]; ok {
					pd.ChokeC <- struct{}{}
				}
			case peerprotocol.InterestedMessage:
				// TODO handle intereseted messages
				_ = pe
			case peerprotocol.NotInterestedMessage:
				// TODO handle not intereseted messages
				_ = pe
			case peerprotocol.PieceMessage:
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("invalid piece index:", msg.Index)
					pe.Peer.Close()
					break
				}
				piece := &d.data.Pieces[msg.Index]
				block := piece.GetBlock(msg.Begin)
				if block == nil {
					pe.Peer.Logger().Errorln("invalid piece begin:", msg.Begin)
					pe.Peer.Close()
					break
				}
				if uint32(len(msg.Data)) != block.Length {
					pe.Peer.Logger().Errorln("invalid piece length:", len(msg.Data))
					pe.Peer.Close()
					break
				}
				pe.bytesDownlaodedInChokePeriod += int64(len(msg.Data))
				if pd, ok := d.pieceDownloads[pe.Peer]; ok {
					pd.PieceC <- piecedownloader.Piece{Block: block, Data: msg.Data}
				}
			case peerprotocol.RequestMessage:
				if d.info == nil {
					pe.Peer.Logger().Error("request received but we don't have info")
					pe.Peer.Close()
					break
				}
				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("invalid request index:", msg.Index)
					pe.Peer.Close()
					break
				}
				if msg.Begin+msg.Length > d.data.Pieces[msg.Index].Length {
					pe.Peer.Logger().Errorln("invalid request length:", msg.Length)
					pe.Peer.Close()
					break
				}
				pi := &d.data.Pieces[msg.Index]
				if pe.amChoking {
					if pe.Peer.FastExtension {
						m := peerprotocol.RejectMessage{RequestMessage: msg}
						pe.SendMessage(m, stopC)
					}
				} else {
					pe.Peer.SendPiece(msg, pi, stopC)
				}
			case peerprotocol.RejectMessage:
				if d.info == nil {
					pe.Peer.Logger().Error("reject received but we don't have info")
					pe.Peer.Close()
					break
				}

				if msg.Index >= uint32(len(d.data.Pieces)) {
					pe.Peer.Logger().Errorln("invalid reject index:", msg.Index)
					pe.Peer.Close()
					break
				}
				piece := &d.data.Pieces[msg.Index]
				block := piece.GetBlock(msg.Begin)
				if block == nil {
					pe.Peer.Logger().Errorln("invalid reject begin:", msg.Begin)
					pe.Peer.Close()
					break
				}
				if msg.Length != block.Length {
					pe.Peer.Logger().Errorln("invalid reject length:", msg.Length)
					pe.Peer.Close()
					break
				}
				pd, ok := d.pieceDownloads[pe.Peer]
				if !ok {
					pe.Peer.Logger().Error("reject received but we don't have active download")
					pe.Peer.Close()
					break
				}
				pd.RejectC <- block
			// TODO make it value type
			case *peerprotocol.ExtensionHandshakeMessage:
				d.log.Debugln("extension handshake received", msg)
				pe.extensionHandshake = msg
				infoDownloaders.Signal(1)
			// TODO make it value type
			case *peerprotocol.ExtensionMetadataMessage:
				switch msg.Type {
				case peerprotocol.ExtensionMetadataMessageTypeRequest:
					if d.info == nil {
						// TODO send reject
						break
					}
					extMsgID, ok := pe.extensionHandshake.M[peerprotocol.ExtensionMetadataKey]
					if !ok {
						// TODO send reject
					}
					// TODO Clients MAY implement flood protection by rejecting request messages after a certain number of them have been served. Typically the number of pieces of metadata times a factor.
					start := 16 * 1024 * msg.Piece
					end := 16 * 1024 * (msg.Piece + 1)
					totalSize := uint32(len(d.info.Bytes))
					if end > totalSize {
						end = totalSize
					}
					data := d.info.Bytes[start:end]
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
					pe.Peer.SendMessage(extDataMsg, stopC)
				case peerprotocol.ExtensionMetadataMessageTypeData:
					id, ok := d.infoDownloads[pe.Peer]
					if !ok {
						pe.Peer.Logger().Warningln("received unexpected metadata piece:", msg.Piece)
						break
					}
					select {
					case id.DataC <- infodownloader.Data{Index: msg.Piece, Data: msg.Data}:
					case <-stopC:
						return
					}
				case peerprotocol.ExtensionMetadataMessageTypeReject:
					// TODO handle metadata piece reject
				}
			default:
				panic(fmt.Sprintf("unhandled peer message type: %T", msg))
			}
		}
	}
}

func (d *Downloader) processInfo() error {
	var err error
	d.data, err = torrentdata.New(d.info, d.dest)
	if err != nil {
		return err
	}
	// TODO defer data.Close()

	if d.bitfield == nil {
		d.bitfield = bitfield.New(d.info.NumPieces)
		if d.data.Exists {
			buf := make([]byte, d.info.PieceLength)
			hash := sha1.New() // nolint: gosec
			for _, p := range d.data.Pieces {
				err := p.Data.ReadFull(buf)
				if err != nil {
					return err
				}
				ok := p.VerifyHash(buf[:p.Length], hash)
				d.bitfield.SetTo(p.Index, ok)
				hash.Reset()
			}
			d.checkCompletion()
		}
	}

	d.preparePieces()
	return nil
}
func (d *Downloader) preparePieces() {
	pieces := make([]Piece, len(d.data.Pieces))
	sortedPieces := make([]*Piece, len(d.data.Pieces))
	for i := range d.data.Pieces {
		pieces[i] = Piece{
			Piece:            &d.data.Pieces[i],
			havingPeers:      make(map[*peer.Peer]*Peer),
			allowedFastPeers: make(map[*peer.Peer]*Peer),
			requestedPeers:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
		sortedPieces[i] = &pieces[i]
	}
	d.pieces = pieces
	d.sortedPieces = sortedPieces
}

func (d *Downloader) nextInfoDownload() *infodownloader.InfoDownloader {
	for _, pe := range d.connectedPeers {
		if pe.infoDownloader != nil {
			continue
		}
		extID, ok := pe.extensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe.Peer, extID, pe.extensionHandshake.MetadataSize)
	}
	return nil
}

func (d *Downloader) nextDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(ByAvailability(d.sortedPieces))
	for _, p := range d.sortedPieces {
		if d.bitfield.Test(p.Index) {
			continue
		}
		if len(p.requestedPeers) > 0 {
			continue
		}
		if p.writing {
			continue
		}
		if len(p.havingPeers) == 0 {
			continue
		}
		// prefer allowed fast peers first
		for _, pe := range p.havingPeers {
			if _, ok := p.allowedFastPeers[pe.Peer]; !ok {
				continue
			}
			if _, ok := d.pieceDownloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer)
		}
		for _, pe := range p.havingPeers {
			if pe.peerChoking {
				continue
			}
			if _, ok := d.pieceDownloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer)
		}
	}
	return nil
}

func (d *Downloader) updateInterestedState(pe *Peer, stopC chan struct{}) {
	if d.info == nil {
		return
	}
	interested := false
	for i := uint32(0); i < d.bitfield.Len(); i++ {
		weHave := d.bitfield.Test(i)
		_, peerHave := d.pieces[i].havingPeers[pe.Peer]
		if !weHave && peerHave {
			interested = true
			break
		}
	}
	if !pe.amInterested && interested {
		pe.amInterested = true
		msg := peerprotocol.InterestedMessage{}
		pe.Peer.SendMessage(msg, stopC)
		return
	}
	if pe.amInterested && !interested {
		pe.amInterested = false
		msg := peerprotocol.NotInterestedMessage{}
		pe.Peer.SendMessage(msg, stopC)
		return
	}
}

func (d *Downloader) chokePeer(pe *Peer, stopC chan struct{}) {
	if !pe.amChoking {
		pe.amChoking = true
		msg := peerprotocol.ChokeMessage{}
		pe.SendMessage(msg, stopC)
	}
}

func (d *Downloader) unchokePeer(pe *Peer, stopC chan struct{}) {
	if pe.amChoking {
		pe.amChoking = false
		msg := peerprotocol.UnchokeMessage{}
		pe.SendMessage(msg, stopC)
	}
}

func (d *Downloader) readMessages(pe *peer.Peer) {
	for msg := range pe.Messages() {
		d.messages <- PeerMessage{Peer: pe, Message: msg}
	}
	d.disconnectedPeers <- pe
}

func (d *Downloader) resendMessages(pe *Peer, stopC chan struct{}) {
	for _, msg := range pe.messages {
		select {
		case d.messages <- PeerMessage{Peer: pe.Peer, Message: msg}:
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) checkCompletion() {
	if d.completed {
		return
	}
	if d.bitfield.All() {
		close(d.completeC)
	}
}
