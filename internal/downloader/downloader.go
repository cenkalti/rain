package downloader

import (
	"sort"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
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
	unchokingPeers map[*peer.Peer]struct{}
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
			Piece:          &d.Pieces[i],
			havingPeers:    make(map[*peer.Peer]struct{}),
			requestedPeers: make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
		sortedPieces[i] = &pieces[i]
	}
	return &Downloader{
		data:           d,
		messages:       m,
		pieces:         pieces,
		sortedPieces:   sortedPieces,
		connectedPeers: make(map[*peer.Peer]*Peer),
		unchokingPeers: make(map[*peer.Peer]struct{}),
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
			pi, pe, ok := d.nextDownload()
			if !ok {
				downloaders.Block()
				continue
			}
			d.log.Debugln("downloading piece", pi.Index, "from", pe.String())
			pd := piecedownloader.New(pi, pe)
			d.downloads[pe] = pd
			d.pieces[pi.Index].requestedPeers[pe] = pd
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
			d.pieces[msg.Piece.Index].havingPeers[msg.Peer] = struct{}{}
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case msg := <-d.messages.Bitfield:
			for i := uint32(0); i < msg.Bitfield.Len(); i++ {
				if msg.Bitfield.Test(i) {
					d.pieces[i].havingPeers[msg.Peer] = struct{}{}
				}
			}
			downloaders.Signal(msg.Bitfield.Count())
			pe := d.connectedPeers[msg.Peer]
			d.updateInterestedState(pe)
		case pe := <-d.messages.Unchoke:
			downloaders.Signal(1)
			d.unchokingPeers[pe] = struct{}{}
			if pd, ok := d.downloads[pe]; ok {
				pd.UnchokeC <- struct{}{}
			}
		case pe := <-d.messages.Choke:
			delete(d.unchokingPeers, pe)
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
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) nextDownload() (pi *piece.Piece, pe *peer.Peer, ok bool) {
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
		// TODO selecting first peer having the piece, change to more smart decision
		for pe2 := range p.havingPeers {
			if _, ok2 := d.unchokingPeers[pe2]; !ok2 {
				continue
			}
			if _, ok2 := d.downloads[pe2]; ok2 {
				continue
			}
			pe = pe2
			break
		}
		if pe == nil {
			continue
		}
		pi = p.Piece
		ok = true
		break
	}
	return
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
