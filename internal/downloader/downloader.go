package downloader

import (
	"sort"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/semaphore"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/worker"
)

const parallelPieceDownloads = 4

const parallelPieceWrites = 4

type Downloader struct {
	data           *torrentdata.Data
	messages       *peer.Messages
	pieces         []Piece
	sortedPieces   []*Piece
	connectedPeers map[*peer.Peer]*Peer
	downloads      map[*peer.Peer]*piecedownloader.PieceDownloader
	downloadDone   chan *piecedownloader.PieceDownloader
	writeRequests  chan piecewriter.Request
	writeResponses chan piecewriter.Response
	errC           chan error
	log            logger.Logger
	workers        worker.Workers
}

func New(d *torrentdata.Data, m *peer.Messages, errC chan error, l logger.Logger) *Downloader {
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
			downloaders.Signal(1)
			d.pieces[msg.Piece.Index].havingPeers[msg.Peer] = d.connectedPeers[msg.Peer]
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case msg := <-d.messages.Bitfield:
			for i := uint32(0); i < msg.Bitfield.Len(); i++ {
				if msg.Bitfield.Test(i) {
					d.pieces[i].havingPeers[msg.Peer] = d.connectedPeers[msg.Peer]
				}
			}
			downloaders.Signal(msg.Bitfield.Count())
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case pe := <-d.messages.HaveAll:
			for i := range d.pieces {
				d.pieces[i].havingPeers[pe] = d.connectedPeers[pe]
			}
			downloaders.Signal(uint32(len(d.pieces)))
			p := d.connectedPeers[pe]
			d.updateInterestedState(p)
		case msg := <-d.messages.AllowedFast:
			d.pieces[msg.Piece.Index].allowedFastPeers[msg.Peer] = d.connectedPeers[msg.Peer]
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
			if pd, ok := d.downloads[msg.Peer]; ok {
				pd.PieceC <- msg
			}
		case msg := <-d.messages.Request:
			go func(msg peer.Request) {
				buf := make([]byte, msg.Length)
				msg.Piece.Data.ReadAt(buf, int64(msg.Begin))
				err := msg.Peer.SendPiece(msg.Piece.Index, msg.Begin, buf)
				if err != nil {
					// TODO ignore write errors
					d.log.Error(err)
				}
			}(msg)
		case pe := <-d.messages.Connect:
			d.connectedPeers[pe] = &Peer{
				Peer:        pe,
				amChoking:   true,
				peerChoking: true,
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
