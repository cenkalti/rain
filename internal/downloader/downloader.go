package downloader

import (
	"bytes"
	"crypto/sha1" // nolint: gosec
	"math/rand"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/downloader/infodownloader"
	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecereader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
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

type Downloader struct {
	infoHash               [20]byte
	dest                   string
	info                   *metainfo.Info
	data                   *torrentdata.Data
	messages               *peer.Messages
	pieces                 []Piece
	sortedPieces           []*Piece
	connectedPeers         map[*peer.Peer]*Peer
	downloads              map[*peer.Peer]*piecedownloader.PieceDownloader
	infoDownloads          map[*peer.Peer]*infodownloader.InfoDownloader
	downloadDone           chan *piecedownloader.PieceDownloader
	infoDownloadDone       chan *infodownloader.InfoDownloader
	writeRequests          chan piecewriter.Request
	writeResponses         chan piecewriter.Response
	readRequests           chan piecereader.Request
	readResponses          chan piecereader.Response
	optimisticUnchokedPeer *Peer
	completeC              chan struct{}
	errC                   chan error
	log                    logger.Logger
	workers                worker.Workers
}

func New(infoHash [20]byte, dest string, info *metainfo.Info, m *peer.Messages, completeC chan struct{}, errC chan error, l logger.Logger) *Downloader {
	return &Downloader{
		infoHash:         infoHash,
		dest:             dest,
		info:             info,
		messages:         m,
		connectedPeers:   make(map[*peer.Peer]*Peer),
		downloads:        make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		infoDownloads:    make(map[*peer.Peer]*infodownloader.InfoDownloader),
		downloadDone:     make(chan *piecedownloader.PieceDownloader),
		infoDownloadDone: make(chan *infodownloader.InfoDownloader),
		writeRequests:    make(chan piecewriter.Request, 1),
		writeResponses:   make(chan piecewriter.Response),
		// TODO queue read requests
		readRequests:  make(chan piecereader.Request, 1),
		readResponses: make(chan piecereader.Response),
		completeC:     completeC,
		errC:          errC,
		log:           l,
	}
}

func (d *Downloader) processInfo() error {
	var err error
	d.data, err = torrentdata.New(d.info, d.dest, d.completeC)
	if err != nil {
		return err
	}
	// TODO defer data.Close()
	err = d.data.Verify()
	if err != nil {
		return err
	}
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
	return nil
}

func (d *Downloader) Run(stopC chan struct{}) {
	defer d.workers.Stop()
	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequests, d.writeResponses, d.log)
		d.workers.Start(w)
	}
	for i := 0; i < parallelPieceReads; i++ {
		w := piecereader.New(d.readRequests, d.readResponses, d.log)
		d.workers.Start(w)
	}
	unchokeTimer := time.NewTicker(10 * time.Second)
	defer unchokeTimer.Stop()
	optimisticUnchokeTimer := time.NewTicker(30 * time.Second)
	defer optimisticUnchokeTimer.Stop()

	downloaders := semaphore.New(parallelPieceDownloads)
	infoDownloaders := semaphore.New(parallelInfoDownloads)
	if d.info == nil {
		downloaders.Block()
	} else {
		infoDownloaders.Block()
		err := d.processInfo()
		if err != nil {
			d.errC <- err
			return
		}
	}

	for {
		select {
		case <-infoDownloaders.Wait:
			id := d.nextInfoDownload()
			if id == nil {
				infoDownloaders.Block()
				continue
			}
			d.log.Debugln("downloading info from", id.Peer.String())
			d.infoDownloads[id.Peer] = id
			d.connectedPeers[id.Peer].infoDownloader = id
			d.workers.StartWithOnFinishHandler(id, func() {
				select {
				case d.infoDownloadDone <- id:
				case <-stopC:
					return
				}
			})
		case id := <-d.infoDownloadDone:
			d.connectedPeers[id.Peer].infoDownloader = nil
			delete(d.infoDownloads, id.Peer)
			select {
			case buf := <-id.DoneC:
				hash := sha1.New() // nolint: gosec
				hash.Write(buf)    // nolint: gosec
				if !bytes.Equal(hash.Sum(nil), d.infoHash[:]) {
					infoDownloaders.Signal(1)
					id.Peer.Close()
					break
				}
				var err error
				d.info, err = metainfo.NewInfo(buf)
				if err != nil {
					d.errC <- err
					return
				}
				err = d.processInfo()
				if err != nil {
					d.errC <- err
					return
				}
				infoDownloaders.Block()
				downloaders.Signal(parallelPieceDownloads)
			}
		// 	// TODO handle error
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
			d.connectedPeers[pd.Peer].downloader = pd
			d.workers.StartWithOnFinishHandler(pd, func() {
				select {
				case d.downloadDone <- pd:
				case <-stopC:
					return
				}
			})
		case pd := <-d.downloadDone:
			d.connectedPeers[pd.Peer].downloader = nil
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
				return
			}
			d.data.Bitfield().Set(resp.Request.Piece.Index)
			d.data.CheckCompletion()
			// Tell everyone that we have this piece
			for _, pe := range d.connectedPeers {
				msg := peerprotocol.HaveMessage{Index: resp.Request.Piece.Index}
				pe.SendMessage(msg, stopC)
				d.updateInterestedState(pe, stopC)
			}
		case resp := <-d.readResponses:
			msg := peerprotocol.PieceMessage{Index: resp.Index, Begin: resp.Begin, Data: resp.Data}
			resp.Peer.SendMessage(msg, stopC)
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
			d.updateInterestedState(pe, stopC)
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
			d.updateInterestedState(pe, stopC)
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
			d.updateInterestedState(p, stopC)
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
		case pe := <-d.messages.Interested:
			// TODO handle intereseted messages
			_ = pe
		case pe := <-d.messages.NotInterested:
			// TODO handle not intereseted messages
			_ = pe
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
						m := peerprotocol.RejectMessage{RequestMessage: msg.RequestMessage}
						pe.SendMessage(m, stopC)
					}
				} else {
					select {
					case d.readRequests <- piecereader.Request{Peer: msg.Peer, Piece: pi, Begin: msg.Begin, Length: msg.Length}:
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
		case p := <-d.messages.Connect:
			pe := NewPeer(p)
			d.connectedPeers[p] = pe
			bf := d.data.Bitfield()
			if p.FastExtension && bf != nil && bf.All() {
				msg := peerprotocol.HaveAllMessage{}
				p.SendMessage(msg, stopC)
			} else if p.FastExtension && bf != nil && bf.Count() == 0 {
				msg := peerprotocol.HaveNoneMessage{}
				p.SendMessage(msg, stopC)
			} else if bf != nil {
				bitfieldData := make([]byte, len(bf.Bytes()))
				copy(bitfieldData, bf.Bytes())
				msg := peerprotocol.BitfieldMessage{Data: bitfieldData}
				p.SendMessage(msg, stopC)
			}
			if len(d.connectedPeers) <= 4 {
				d.unchokePeer(pe, stopC)
			}
		case p := <-d.messages.Disconnect:
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
		}
	}
}

func (d *Downloader) nextInfoDownload() *infodownloader.InfoDownloader {
	// TODO implement
	return nil
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

func (d *Downloader) updateInterestedState(pe *Peer, stopC chan struct{}) {
	if d.info == nil {
		return
	}
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
