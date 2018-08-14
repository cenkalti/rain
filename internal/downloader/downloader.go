package downloader

import (
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

// Request pieces in blocks of this size.
const blockSize = 16 * 1024

const parallelPieceDownloads = 10

const maxQueuedBlocks = 10

type Downloader struct {
	peerManager *peermanager.PeerManager
	data        *torrentdata.Data
	bitfield    *bitfield.Bitfield
	log         logger.Logger
}

type Piece struct {
	*piece.Piece
	index       int
	havingPeers map[*peer.Peer]struct{}
	downloads   map[*peer.Peer]*pieceDownloader
}

type Peer struct {
	*peer.Peer
}

func New(pm *peermanager.PeerManager, d *torrentdata.Data, b *bitfield.Bitfield, l logger.Logger) *Downloader {
	return &Downloader{
		peerManager: pm,
		data:        d,
		bitfield:    b,
		log:         l,
	}
}

func (d *Downloader) Run(stopC chan struct{}) {
	pieces := make([]Piece, len(d.data.Pieces))
	for i := range d.data.Pieces {
		pieces[i] = Piece{
			Piece:       &d.data.Pieces[i],
			index:       i,
			havingPeers: make(map[*peer.Peer]struct{}),
			downloads:   make(map[*peer.Peer]*pieceDownloader),
		}
	}

	var activeDownloads []*pieceDownloader

	for {
		select {
		case <-time.After(time.Second):
			// TODO selecting pieces in sequential order, change to rarest first
			for i, p := range pieces {
				if len(activeDownloads) >= parallelPieceDownloads {
					break
				}
				if d.bitfield.Test(uint32(i)) {
					continue
				}
				if len(p.havingPeers) == 0 {
					continue
				}
				var havingPeer *peer.Peer
				// TODO selecting first peer having the piece, change to more smart decision
				for havingPeer = range p.havingPeers {
					break
				}
				if havingPeer == nil {
					continue
				}
				req := newPieceDownloader(&pieces[i], &Peer{Peer: havingPeer})
				d.log.Debugln("downloading piece", req.piece.index, "from", req.peer)
				activeDownloads = append(activeDownloads, req)
				pieces[i].downloads[req.peer.Peer] = req
				go req.run(stopC)
			}
		case pm := <-d.peerManager.PeerMessages():
			switch msg := pm.Message.(type) {
			case peer.Have:
				pieces[msg.Index].havingPeers[pm.Peer] = struct{}{}
			case peer.Choke:
				// for _, p := range pieces {
				// 	delete(p.havingPeers, pm.Peer)
				// }
			case peer.Piece:
				// TODO handle piece message
				// pd := pieces[msg.Index].downloads[pm.Peer]
				// pd.pieceC <- msg.
			}
		case <-stopC:
			return
		}
	}
}
