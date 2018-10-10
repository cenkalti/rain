package torrent

import (
	"sort"

	"github.com/cenkalti/rain/torrent/internal/infodownloader"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/piece"
	"github.com/cenkalti/rain/torrent/internal/piecedownloader"
)

func (t *Torrent) nextInfoDownload() *infodownloader.InfoDownloader {
	for _, pe := range t.connectedPeers {
		if pe.InfoDownloader != nil {
			continue
		}
		extID, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionMetadataKey]
		if !ok {
			continue
		}
		return infodownloader.New(pe.Conn, extID, pe.ExtensionHandshake.MetadataSize, t.infoDownloaderResultC)
	}
	return nil
}

func (t *Torrent) nextPieceDownload() *piecedownloader.PieceDownloader {
	// TODO request first 4 pieces randomly
	sort.Sort(piece.ByAvailability(t.sortedPieces))
	for _, p := range t.sortedPieces {
		if t.bitfield.Test(p.Index) {
			continue
		}
		if len(p.RequestedPeers) > 0 {
			continue
		}
		if p.Writing {
			continue
		}
		if len(p.HavingPeers) == 0 {
			continue
		}
		// prefer allowed fast peers first
		for pe := range p.HavingPeers {
			if _, ok := p.AllowedFastPeers[pe]; !ok {
				continue
			}
			if _, ok := t.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, t.pieceDownloaderResultC)
		}
		for pe := range p.HavingPeers {
			if pp, ok := t.connectedPeers[pe]; ok && pp.PeerChoking {
				continue
			}
			if _, ok := t.pieceDownloads[pe]; ok {
				continue
			}
			// TODO selecting first peer having the piece, change to more smart decision
			return piecedownloader.New(p.Piece, pe, t.pieceDownloaderResultC)
		}
	}
	return nil
}
