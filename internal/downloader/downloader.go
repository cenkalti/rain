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
	data          *torrentdata.Data
	messages      *peer.Messages
	pieces        []Piece
	unchokedPeers map[*peer.Peer]struct{}
	downloads     map[*peer.Peer]*piecedownloader.PieceDownloader
	log           logger.Logger
	limiter       chan struct{}
	m             sync.Mutex
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
		data:          d,
		messages:      m,
		pieces:        pieces,
		unchokedPeers: make(map[*peer.Peer]struct{}),
		downloads:     make(map[*peer.Peer]*piecedownloader.PieceDownloader),
		log:           l,
		limiter:       make(chan struct{}, parallelPieceDownloads),
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	for {
		// TODO extract cases to methods
		select {
		case d.limiter <- struct{}{}:
			// TODO check status of existing downloads
			go func() {
				d.m.Lock()
				pi, pe, ok := d.nextDownload()
				if !ok {
					d.m.Unlock()
					select {
					case msg := <-d.messages.Have:
						go func() {
							select {
							case d.messages.Have <- msg:
							case <-stopC:
								return
							}
						}()
					case msg := <-d.messages.Unchoke:
						go func() {
							select {
							case d.messages.Unchoke <- msg:
							case <-stopC:
								return
							}
						}()
					case <-stopC:
						return
					}
					<-d.limiter
					return
					// TODO separate channels for each message type
				}
				d.startDownload(pi, pe, stopC)
				d.m.Unlock()
			}()
		case msg := <-d.messages.Have:
			d.pieces[msg.Index].havingPeers[msg.Peer] = struct{}{}
			// go checkInterested(peer, bitfield)
			// peer.writeMessages <- interested{}
		case pe := <-d.messages.Unchoke:
			d.unchokedPeers[pe] = struct{}{}
			if pd, ok := d.downloads[pe]; ok {
				pd.UnchokeC <- struct{}{}
			}
		case pe := <-d.messages.Unchoke:
			delete(d.unchokedPeers, pe)
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
			err := msg.Peer.SendPiece(msg.Index, msg.Begin, buf)
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
			if _, ok2 := d.unchokedPeers[pe2]; !ok2 {
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

func (d *Downloader) startDownload(pi *piece.Piece, pe *peer.Peer, stopC chan struct{}) {
	d.log.Debugln("downloading piece", pi.Index, "from", pe.String())
	pd := piecedownloader.New(pi, pe)
	d.downloads[pe] = pd
	d.pieces[pi.Index].requestedPeers[pe] = pd
	go d.downloadPiece(pd, stopC)
}

func (d *Downloader) downloadPiece(pd *piecedownloader.PieceDownloader, stopC chan struct{}) {
	defer func() {
		d.m.Lock()
		delete(d.downloads, pd.Peer)
		delete(d.pieces[pd.Piece.Index].requestedPeers, pd.Peer)
		d.m.Unlock()
		<-d.limiter
	}()
	buf, err := pd.Run(stopC)
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
