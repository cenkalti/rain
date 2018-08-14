package downloader

import (
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

const parallelPieceDownloads = 4

const maxQueuedBlocks = 10

type Downloader struct {
	peerManager *peermanager.PeerManager
	data        *torrentdata.Data
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

func New(pm *peermanager.PeerManager, d *torrentdata.Data, l logger.Logger) *Downloader {
	return &Downloader{
		peerManager: pm,
		data:        d,
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
		// TODO extract cases to methods
		select {
		case <-time.After(time.Second):
			// TODO selecting pieces in sequential order, change to rarest first
			for i, p := range pieces {
				if len(activeDownloads) >= parallelPieceDownloads {
					break
				}
				if p.OK {
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
				// pd.blockC <- msg.Block

				// pd.pieceC <- msg.

				// 				p.log.Debugf("Writing piece to disk: #%d", piece.Index)
				// 				if _, err = piece.Write(active.Data); err != nil {
				// 					p.log.Error(err)
				// 					// TODO remove errcheck ignore
				// 					p.conn.Close() // nolint: errcheck
				// 					return
				// 				}

				// 				p.torrent.m.Lock()
				// 				p.torrent.bitfield.Set(piece.Index)
				// 				percentDone := p.torrent.bitfield.Count() * 100 / p.torrent.bitfield.Len()
				// 				p.torrent.m.Unlock()
				// 				p.cond.Broadcast()
				// 				p.torrent.log.Infof("Completed: %d%%", percentDone)
			}
		case <-stopC:
			return
		}
	}
}
