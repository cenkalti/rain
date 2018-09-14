package downloader

import (
	"math/rand"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader/peerwriter"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/worker"
)

const parallelPieceDownloads = 4

const parallelPieceWrites = 4

type Downloader struct {
	info                   *metainfo.Info
	data                   *torrentdata.Data
	messages               *peer.Messages
	pieces                 []Piece
	sortedPieces           []*Piece
	connectedPeers         map[*peer.Peer]*Peer
	downloads              map[*peer.Peer]*piecedownloader.PieceDownloader
	downloadDone           chan *piecedownloader.PieceDownloader
	writeRequests          chan piecewriter.Request
	writeResponses         chan piecewriter.Response
	optimisticUnchokedPeer *Peer
	errC                   chan error
	log                    logger.Logger
	workers                worker.Workers
}

func New(info *metainfo.Info, d *torrentdata.Data, m *peer.Messages, errC chan error, l logger.Logger) *Downloader {
	pieces := make([]Piece, len(d.Pieces))
	sortedPieces := make([]*Piece, len(d.Pieces))
	for i := range d.Pieces {
		pieces[i] = Piece{
			Piece:            &d.Pieces[i],
			havingPeers:      make(map[*peer.Peer]*Peer),
			allowedFastPeers: make(map[*peer.Peer]*Peer),
			requestedPeers:   make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
		sortedPieces[i] = &pieces[i]
	}
	return &Downloader{
		info:           info,
		data:           d,
		messages:       m,
		pieces:         pieces,
		sortedPieces:   sortedPieces,
		connectedPeers: make(map[*peer.Peer]*Peer),
		downloads:      make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		downloadDone:   make(chan *piecedownloader.PieceDownloader),
		writeRequests:  make(chan piecewriter.Request, 1),
		writeResponses: make(chan piecewriter.Response),
		errC:           errC,
		log:            l,
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	defer d.workers.Stop()
	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequests, d.writeResponses, d.log)
		d.workers.Start(w)
	}
	downloaders := semaphore.New(parallelPieceDownloads)
	unchokeTimer := time.NewTicker(10 * time.Second)
	defer unchokeTimer.Stop()
	optimisticUnchokeTimer := time.NewTicker(30 * time.Second)
	defer optimisticUnchokeTimer.Stop()
	for {
		select {
		case <-downloaders.Wait:
			// TODO check status of existing downloads
			pd := d.nextDownload()
			if pd == nil {
				downloaders.Block()
				continue
			}
			d.log.Debugln("downloading piece", pd.Piece.Index, "from", pd.Peer.String())
			d.downloads[pd.Peer] = pd
			d.pieces[pd.Piece.Index].requestedPeers[pd.Peer] = pd
			d.workers.StartWithOnFinishHandler(pd, func() {
				select {
				case d.downloadDone <- pd:
				case <-stopC:
					return
				}
			})
		case pd := <-d.downloadDone:
			delete(d.downloads, pd.Peer)
			delete(d.pieces[pd.Piece.Index].requestedPeers, pd.Peer)
			downloaders.Signal(1)
			select {
			case buf := <-pd.DoneC:
				ok := d.pieces[pd.Piece.Index].Piece.Verify(buf)
				if !ok {
					// TODO handle corrupt piece
					continue
				}
				select {
				case d.writeRequests <- piecewriter.Request{Piece: pd.Piece, Data: buf}:
					d.pieces[pd.Piece.Index].writing = true
				case <-stopC:
					return
				}
			case err := <-pd.ErrC:
				d.log.Errorln("could not download piece:", err)
			case <-stopC:
				return
			}
		case resp := <-d.writeResponses:
			d.pieces[resp.Request.Piece.Index].writing = false
			if resp.Error != nil {
				d.errC <- resp.Error
				continue
			}
			d.data.Bitfield().Set(resp.Request.Piece.Index)
			d.data.CheckCompletion()
			// Tell everyone that we have this piece
			for _, pe := range d.connectedPeers {
				go pe.SendHave(resp.Request.Piece.Index)
				d.updateInterestedState(pe)
			}
		case msg := <-d.messages.Have:
			// Save have messages for processesing later received while we don't have info yet.
			if d.info == nil {
				pe := d.connectedPeers[msg.Peer]
				pe.haveMessages = append(pe.haveMessages, msg.HaveMessage)
				break
			}
			if msg.Index >= uint32(len(d.data.Pieces)) {
				msg.Peer.Logger().Errorln("unexpected piece index:", msg.Index)
				msg.Peer.Close()
				break
			}
			pi := &d.data.Pieces[msg.Index]
			msg.Peer.Logger().Debug("Peer ", msg.Peer.String(), " has piece #", pi.Index)
			downloaders.Signal(1)
			d.pieces[pi.Index].havingPeers[msg.Peer] = d.connectedPeers[msg.Peer]
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case msg := <-d.messages.Bitfield:
			// Save bitfield messages while we don't have info yet.
			if d.info == nil {
				pe := d.connectedPeers[msg.Peer]
				pe.bitfieldMessage = msg.Data
				break
			}
			numBytes := uint32(bitfield.NumBytes(uint32(len(d.data.Pieces))))
			if uint32(len(msg.Data)) != numBytes {
				msg.Peer.Logger().Errorln("invalid bitfield length:", len(msg.Data))
				msg.Peer.Close()
				break
			}
			bf := bitfield.NewBytes(msg.Data, uint32(len(d.data.Pieces)))
			msg.Peer.Logger().Debugln("Received bitfield:", bf.Hex())
			for i := uint32(0); i < bf.Len(); i++ {
				if bf.Test(i) {
					d.pieces[i].havingPeers[msg.Peer] = d.connectedPeers[msg.Peer]
				}
			}
			downloaders.Signal(bf.Count())
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case pe := <-d.messages.HaveAll:
			if d.info == nil {
				pe := d.connectedPeers[pe]
				pe.haveAllMessage = true
				break
			}
			for i := range d.pieces {
				d.pieces[i].havingPeers[pe] = d.connectedPeers[pe]
			}
			downloaders.Signal(uint32(len(d.pieces)))
			p := d.connectedPeers[pe]
			d.updateInterestedState(p)
		case msg := <-d.messages.AllowedFast:
			if d.info == nil {
				pe := d.connectedPeers[msg.Peer]
				pe.allowedFastMessages = append(pe.allowedFastMessages, msg.HaveMessage)
				break
			}
			if msg.Index >= uint32(len(d.data.Pieces)) {
				msg.Peer.Logger().Errorln("invalid allowed fast piece index:", msg.Index)
				msg.Peer.Close()
				break
			}
			pi := &d.data.Pieces[msg.Index]
			msg.Peer.Logger().Debug("Peer ", msg.Peer.String(), " has allowed fast for piece #", pi.Index)
			d.pieces[msg.Index].allowedFastPeers[msg.Peer] = d.connectedPeers[msg.Peer]
		case pe := <-d.messages.Unchoke:
			downloaders.Signal(1)
			d.connectedPeers[pe].peerChoking = false
			if pd, ok := d.downloads[pe]; ok {
				pd.UnchokeC <- struct{}{}
			}
		case pe := <-d.messages.Choke:
			d.connectedPeers[pe].peerChoking = true
			if pd, ok := d.downloads[pe]; ok {
				pd.ChokeC <- struct{}{}
			}
		case msg := <-d.messages.Piece:
			if msg.Index >= uint32(len(d.data.Pieces)) {
				msg.Peer.Logger().Errorln("invalid piece index:", msg.Index)
				msg.Peer.Close()
				break
			}
			piece := &d.data.Pieces[msg.Index]
			block := piece.GetBlock(msg.Begin)
			if block == nil {
				msg.Peer.Logger().Errorln("invalid piece begin:", msg.Begin)
				msg.Peer.Close()
				break
			}
			if uint32(len(msg.Data)) != block.Length {
				msg.Peer.Logger().Errorln("invalid piece length:", len(msg.Data))
				msg.Peer.Close()
				break
			}
			if pe, ok := d.connectedPeers[msg.Peer]; ok {
				pe.bytesDownlaodedInChokePeriod += int64(len(msg.Data))
			}
			if pd, ok := d.downloads[msg.Peer]; ok {
				pd.PieceC <- piecedownloader.Piece{Block: block, Data: msg.Data}
			}
		case msg := <-d.messages.Request:
			if d.info == nil {
				msg.Peer.Logger().Error("request received but we don't have info")
				msg.Peer.Close()
				break
			}
			if msg.Index >= uint32(len(d.data.Pieces)) {
				msg.Peer.Logger().Errorln("invalid request index:", msg.Index)
				msg.Peer.Close()
				break
			}
			if msg.Begin+msg.Length > d.data.Pieces[msg.Index].Length {
				msg.Peer.Logger().Errorln("invalid request length:", msg.Length)
				msg.Peer.Close()
				break
			}
			pi := &d.data.Pieces[msg.Index]
			if pe, ok := d.connectedPeers[msg.Peer]; ok {
				if pe.amChoking {
					if msg.Peer.FastExtension {
						d.rejectPiece(pe, msg)
					}
				} else {
					select {
					case pe.writer.RequestC <- peerwriter.Request{Piece: pi, Request: msg}:
					case <-stopC:
						return
					}
				}
			}
		case msg := <-d.messages.Reject:
			if d.info == nil {
				msg.Peer.Logger().Error("reject received but we don't have info")
				msg.Peer.Close()
				break
			}

			if msg.Index >= uint32(len(d.data.Pieces)) {
				msg.Peer.Logger().Errorln("invalid reject index:", msg.Index)
				msg.Peer.Close()
				break
			}
			piece := &d.data.Pieces[msg.Index]
			block := piece.GetBlock(msg.Begin)
			if block == nil {
				msg.Peer.Logger().Errorln("invalid reject begin:", msg.Begin)
				msg.Peer.Close()
				break
			}
			if msg.Length != block.Length {
				msg.Peer.Logger().Errorln("invalid reject length:", msg.Length)
				msg.Peer.Close()
				break
			}
			pd, ok := d.downloads[msg.Peer]
			if !ok {
				msg.Peer.Logger().Error("reject received but we don't have active download")
				msg.Peer.Close()
				break
			}
			pd.RejectC <- block
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
				d.unchokePeer(pe)
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
			d.unchokePeer(pe)
			d.optimisticUnchokedPeer = pe
		case p := <-d.messages.Connect:
			pe := NewPeer(p)
			d.workers.Start(pe)
			d.connectedPeers[p] = pe
			pe.writer.BitfieldC <- d.data.Bitfield()
			if len(d.connectedPeers) <= 4 {
				d.unchokePeer(pe)
			}
		case pe := <-d.messages.Disconnect:
			delete(d.connectedPeers, pe)
			for i := range d.pieces {
				delete(d.pieces[i].havingPeers, pe)
				delete(d.pieces[i].allowedFastPeers, pe)
				delete(d.pieces[i].requestedPeers, pe)
			}
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) nextDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(ByAvailability(d.sortedPieces))
	for _, p := range d.sortedPieces {
		if d.data.Bitfield().Test(p.Index) {
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
			if _, ok := d.downloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer)
		}
		for _, pe := range p.havingPeers {
			if pe.peerChoking {
				continue
			}
			if _, ok := d.downloads[pe.Peer]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe.Peer)
		}
	}
	return nil
}

func (d *Downloader) updateInterestedState(pe *Peer) {
	interested := false
	for i := uint32(0); i < d.data.Bitfield().Len(); i++ {
		weHave := d.data.Bitfield().Test(i)
		_, peerHave := d.pieces[i].havingPeers[pe.Peer]
		if !weHave && peerHave {
			interested = true
			break
		}
	}
	if !pe.amInterested && interested {
		pe.amInterested = true
		go pe.Peer.SendInterested()
		return
	}
	if pe.amInterested && !interested {
		pe.amInterested = false
		go pe.Peer.SendNotInterested()
		return
	}
}

func (d *Downloader) chokePeer(pe *Peer, stopC chan struct{}) {
	if !pe.amChoking {
		pe.amChoking = true
		select {
		case pe.writer.ChokeC <- struct{}{}:
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) unchokePeer(pe *Peer) {
	if pe.amChoking {
		pe.amChoking = false
		go pe.Peer.SendUnchoke()
	}
}

func (d *Downloader) sendPiece(pe *Peer, msg peer.Request, pi *piece.Piece) {
	buf := make([]byte, msg.Length)
	err := pi.Data.ReadAt(buf, int64(msg.Begin))
	if err != nil {
		// TODO handle cannot read piece data for uploading
		return
	}
	msg.Peer.SendPiece(msg.Index, msg.Begin, buf)
}

func (d *Downloader) rejectPiece(pe *Peer, msg peer.Request) {
	go msg.Peer.SendReject(msg.Index, msg.Begin, msg.Length)
}
