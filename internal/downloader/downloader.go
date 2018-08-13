package downloader

import (
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/peermanager"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/torrentdata"
)

type Downloader struct {
	peerManager  *peermanager.PeerManager
	data         *torrentdata.Data
	bitfield     *bitfield.Bitfield
	peerMessages chan peer.Message
}

// type pieceState struct {
// 	p *piece.Piece
// 	requested map[*peer]
// }

type downloaderPiece struct {
	piece       *piece.Piece
	havingPeers map[*peer.Peer]struct{}
}

type downloaderRequest struct {
}

func New(pm *peermanager.PeerManager, d *torrentdata.Data, b *bitfield.Bitfield) *Downloader {
	return &Downloader{
		peerManager:  pm,
		data:         d,
		bitfield:     b,
		peerMessages: make(chan peer.Message),
	}
}

func (d *Downloader) PeerMessages() chan peer.Message {
	return d.peerMessages
}

// TODO implement
func (d *Downloader) Run(stopC chan struct{}) {
	pieces := make([]downloaderPiece, len(d.data.Pieces))
	for _, p := range pieces {
		p.havingPeers = make(map[*peer.Peer]struct{})
	}

	// var requests []*downloaderRequest

	for {
		select {
		case <-time.After(time.Second):
			// TODO selecting pieces in sequential order, change to rarest first
			for i, p := range pieces {
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
				go downloadPiece(p.piece, havingPeer)
			}

			// for _, p := range t.data.Pieces() {
			// jjjkk
			// missingPiece := findMissingPiece()
			// if missingPiece == nil {
			// 	continue
			// }
			// havingPeer := findHavingPeer()
			// if havingPeer == nil {
			// 	continue
			// }
		case pm := <-d.peerMessages:
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

func downloadPiece(pi *piece.Piece, pe *peer.Peer) {
	// blocksRequested := bitfield.New(uint32(len(pi.Blocks)))
	// for
}
