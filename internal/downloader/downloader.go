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

type Downloader struct {
	peerManager *peermanager.PeerManager
	data        *torrentdata.Data
	bitfield    *bitfield.Bitfield
	log         logger.Logger
}

type downloaderPiece struct {
	index       int
	piece       *piece.Piece
	havingPeers map[*peer.Peer]struct{}
}

type pieceDownload struct {
	piece *downloaderPiece
	peer  *peer.Peer
}

func New(pm *peermanager.PeerManager, d *torrentdata.Data, b *bitfield.Bitfield, l logger.Logger) *Downloader {
	return &Downloader{
		peerManager: pm,
		data:        d,
		bitfield:    b,
		log:         l,
	}
}

// TODO implement
func (d *Downloader) Run(stopC chan struct{}) {
	pieces := make([]downloaderPiece, len(d.data.Pieces))
	for i := range d.data.Pieces {
		pieces[i] = downloaderPiece{
			index:       i,
			piece:       &d.data.Pieces[i],
			havingPeers: make(map[*peer.Peer]struct{}),
		}
	}

	var activeDownloads []*pieceDownload

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
				req := &pieceDownload{&pieces[i], havingPeer}
				activeDownloads = append(activeDownloads, req)
				go d.downloadPiece(req)
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
			}
		case <-stopC:
			return
		}
	}
}

func (d *Downloader) downloadPiece(req *pieceDownload) {
	d.log.Debugln("downloading piece", req.piece.index, "from", req.peer)
	// blocksRequested := bitfield.New(uint32(len(pi.Blocks)))
	// for
}
