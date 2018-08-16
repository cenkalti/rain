package downloader

import (
	"sync"

	"github.com/cenkalti/rain/internal/downloader/piecedownloader"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

const parallelPieceDownloads = 4

type Downloader struct {
	peerManager   *peermanager.PeerManager
	data          *torrentdata.Data
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

func New(pm *peermanager.PeerManager, d *torrentdata.Data, l logger.Logger) *Downloader {
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
		peerManager:   pm,
		data:          d,
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
			d.m.Lock()
			pi, pe, ok := d.nextDownload()
			if !ok {
				d.m.Unlock()
				// TODO separate channels for each message type
				// select {
				// case pm := <-d.peerManager.PeerMessages():
				// 	<-d.limiter
				// 	go func() { d.peerManager.PeerMessages() <- pm }()
				// case <-stopC:
				// 	return
				// }
				<-d.limiter
				break
			}
			d.startDownload(pi, pe, stopC)
			d.m.Unlock()
		case pm := <-d.peerManager.PeerMessages():
			switch msg := pm.Message.(type) {
			case peer.Have:
				d.pieces[msg.Index].havingPeers[pm.Peer] = struct{}{}
			case peer.Unchoke:
				d.unchokedPeers[pm.Peer] = struct{}{}
				if pd, ok := d.downloads[pm.Peer]; ok {
					pd.UnchokeC <- struct{}{}
				}
			case peer.Choke:
				delete(d.unchokedPeers, pm.Peer)
				if pd, ok := d.downloads[pm.Peer]; ok {
					pd.ChokeC <- struct{}{}
				}
			case peer.Piece:
				if pd, ok := d.downloads[pm.Peer]; ok {
					pd.PieceC <- msg
				}
			case peer.Request:
				pe := pm.Peer
				// pi := d.data.Pieces[msg.Index]
				buf := make([]byte, msg.Length)
				// pi.Data.ReadAt(buf, int64(msg.Begin))
				err := pe.SendPiece(msg.Index, msg.Begin, buf)
				if err != nil {
					d.log.Error(err)
					return
				}
			default:
				d.log.Debugln("unhandled message type:", msg)
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
