package downloader

import (
	"sync"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/downloader/piecewriter"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
	"github.com/cenkalti/rain/internal/worker"
)

const parallelPieceDownloads = 4

const parallelPieceWrites = 4

type Downloader struct {
	data           *torrentdata.Data
	messages       *peer.Messages
	pieces         []Piece
	connectedPeers map[*peer.Peer]struct{}
	unchokingPeers map[*peer.Peer]struct{}
	downloads      map[*peer.Peer]*piecedownloader.PieceDownloader
	downloadDone   chan *piecedownloader.PieceDownloader
	writeRequests  chan piecewriter.Request
	writeResponses chan piecewriter.Response
	errC           chan error
	log            logger.Logger
	limiter        chan struct{}
	workers        worker.Workers
	m              sync.Mutex
}

type Piece struct {
	*piece.Piece
	havingPeers    map[*peer.Peer]struct{}
	requestedPeers map[*peer.Peer]*piecedownloader.PieceDownloader
	writing        bool
}

func New(d *torrentdata.Data, m *peer.Messages, errC chan error, l logger.Logger) *Downloader {
	pieces := make([]Piece, len(d.Pieces))
	for i := range d.Pieces {
		pieces[i] = Piece{
			Piece:          &d.Pieces[i],
			havingPeers:    make(map[*peer.Peer]struct{}),
			requestedPeers: make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
	}
	return &Downloader{
		data:           d,
		messages:       m,
		pieces:         pieces,
		connectedPeers: make(map[*peer.Peer]struct{}),
		unchokingPeers: make(map[*peer.Peer]struct{}),
		downloads:      make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		downloadDone:   make(chan *piecedownloader.PieceDownloader),
		writeRequests:  make(chan piecewriter.Request, 1),
		writeResponses: make(chan piecewriter.Response),
		errC:           errC,
		log:            l,
		limiter:        make(chan struct{}, parallelPieceDownloads),
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	defer d.workers.Stop()
	for i := 0; i < parallelPieceWrites; i++ {
		w := piecewriter.New(d.writeRequests, d.writeResponses, d.log)
		d.workers.Start(w)
	}
	waitingDownloader := 0
	for {
		select {
		case d.limiter <- struct{}{}:
			// TODO check status of existing downloads
			pi, pe, ok := d.nextDownload()
			if !ok {
				waitingDownloader++
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
			<-d.limiter
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
			for pe := range d.connectedPeers {
				go pe.SendHave(resp.Request.Piece.Index)
			}
			// TODO update interested state
		case msg := <-d.messages.Have:
			if waitingDownloader > 0 {
				waitingDownloader--
				<-d.limiter
			}
			d.pieces[msg.Piece.Index].havingPeers[msg.Peer] = struct{}{}
			// TODO update interested state
			// go checkInterested(peer, bitfield)
			// peer.writeMessages <- interested{}
		case pe := <-d.messages.Unchoke:
			if waitingDownloader > 0 {
				waitingDownloader--
				<-d.limiter
			}
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
			// pi := d.data.Pieces[msg.Index]
			buf := make([]byte, msg.Length)
			// TODO read data
			// pi.Data.ReadAt(buf, int64(msg.Begin))
			err := msg.Peer.SendPiece(msg.Piece.Index, msg.Begin, buf)
			if err != nil {
				d.log.Error(err)
				return
			}
		case pe := <-d.messages.Connect:
			d.connectedPeers[pe] = struct{}{}
		case pe := <-d.messages.Disconnect:
			delete(d.connectedPeers, pe)
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) nextDownload() (pi *piece.Piece, pe *peer.Peer, ok bool) {
	// TODO selecting pieces in sequential order, change to rarest first
	for _, p := range d.pieces {
		if d.data.Bitfield().Test(p.Index) {
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
			if _, ok2 := p.requestedPeers[pe2]; ok2 {
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
