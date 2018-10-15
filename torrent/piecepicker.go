package torrent

import (
	"sort"

	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
)

func (t *Torrent) nextInfoDownload() *infodownloader.InfoDownloader {
	for pe := range t.peers {
		if _, ok := t.infoDownloaders[pe]; ok {
			continue
		}
		extID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe, extID, pe.ExtensionHandshake.MetadataSize, t.snubbedInfoDownloaderC, t.infoDownloaderDoneC)
	}
	return nil
}

func (t *Torrent) nextPieceDownload() *piecedownloader.PieceDownloader {
	pi, pe := t.findPieceAndPeer()
	if pi == nil || pe == nil {
		return nil
	}
	return piecedownloader.New(pi.Piece, pe, t.snubbedPieceDownloaderC, t.pieceDownloaderDoneC)
}

func (t *Torrent) findPieceAndPeer() (*piece.Piece, *peer.Peer) {
	// TODO request first 4 pieces randomly
	sort.Sort(piece.ByAvailability(t.sortedPieces))
	for _, pi := range t.sortedPieces {
		if t.bitfield.Test(pi.Index) {
			continue
		}
		if len(pi.RequestedPeers) > 0 {
			continue
		}
		if len(pi.HavingPeers) == 0 {
			continue
		}
		// prefer not snubbed peers first, then prefer allowed fast peers
		pe := t.selectPeer(pi, true, true)
		if pe != nil {
			return pi, pe
		}
		pe = t.selectPeer(pi, true, false)
		if pe != nil {
			return pi, pe
		}
		pe = t.selectPeer(pi, false, true)
		if pe != nil {
			return pi, pe
		}
		pe = t.selectPeer(pi, false, false)
		if pe != nil {
			return pi, pe
		}
	}
	return nil, nil
}

func (t *Torrent) selectPeer(pi *piece.Piece, skipSnubbed, allowedFastOnly bool) *peer.Peer {
	for pe := range pi.HavingPeers {
		if _, ok := t.pieceDownloaders[pe]; ok {
			continue
		}
		if skipSnubbed {
			if pe.Snubbed {
				continue
			}
		}
		if allowedFastOnly {
			if _, ok := pi.AllowedFastPeers[pe]; !ok {
				continue
			}
		} else {
			if pe.PeerChoking {
				continue
			}
		}
		pe.Snubbed = false
		return pe
	}
	return nil
}
