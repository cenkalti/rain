package downloader

import (
	"sync"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

const parallelPieceDownloads = 4

type Downloader struct {
	data           *torrentdata.Data
	messages       *peer.Messages
	pieces         []Piece
	unchokingPeers map[*peer.Peer]struct{}
	downloads      map[*peer.Peer]*piecedownloader.PieceDownloader
	downloadDone   chan *piecedownloader.PieceDownloader
	log            logger.Logger
	limiter        chan struct{}
	m              sync.Mutex
}

type Piece struct {
	*piece.Piece
	index          int
	havingPeers    map[*peer.Peer]struct{}
	requestedPeers map[*peer.Peer]*piecedownloader.PieceDownloader
}

func New(d *torrentdata.Data, m *peer.Messages, l logger.Logger) *Downloader {
	pieces := make([]Piece, len(d.Pieces))
	for i := range d.Pieces {
		pieces[i] = Piece{
			Piece:          &d.Pieces[i],
			index:          i,
			havingPeers:    make(map[*peer.Peer]struct{}),
			requestedPeers: make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		}
	}
	return &Downloader{
		data:           d,
		messages:       m,
		pieces:         pieces,
		unchokingPeers: make(map[*peer.Peer]struct{}),
		downloads:      make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		downloadDone:   make(chan *piecedownloader.PieceDownloader),
		log:            l,
		limiter:        make(chan struct{}, parallelPieceDownloads),
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	waitingDownloader := 0
	for {
		// TODO extract cases to methods
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
			go d.downloadPiece(pd, stopC)
		case pd := <-d.downloadDone:
			delete(d.downloads, pd.Peer)
			delete(d.pieces[pd.Piece.Index].requestedPeers, pd.Peer)
			<-d.limiter
		case msg := <-d.messages.Have:
			if waitingDownloader > 0 {
				waitingDownloader--
				<-d.limiter
			}
			d.pieces[msg.Piece.Index].havingPeers[msg.Peer] = struct{}{}
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
			// pi.Data.ReadAt(buf, int64(msg.Begin))
			err := msg.Peer.SendPiece(msg.Piece.Index, msg.Begin, buf)
			if err != nil {
				d.log.Error(err)
				return
			}
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

func (d *Downloader) downloadPiece(pd *piecedownloader.PieceDownloader, stopC chan struct{}) {
	defer func() {
		select {
		case d.downloadDone <- pd:
		case <-stopC:
		}
	}()
	buf, err := pd.Download(stopC)
	if err != nil {
		d.log.Error(err)
		return
	}
	err = d.data.WritePiece(pd.Piece.Index, buf.Bytes())
	if err != nil {
		// TODO blacklist peer if sent corrupt piece
		d.log.Error(err)
		return
	}
}
