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
		if pe.ExtensionHandshake == nil {
			continue
		}
		if pe.ExtensionHandshake.MetadataSize == 0 {
			continue
		}
		_, ok := pe.ExtensionHandshake.M[peerprotocol.ExtensionKeyMetadata]
		if !ok {
			continue
		}
		return infodownloader.New(pe)
	}
	return nil
}

func (t *Torrent) nextPieceDownload() *piecedownloader.PieceDownloader {
	pi, pe := t.findPieceAndPeer()
	if pi == nil || pe == nil {
		return nil
	}
	pe.Snubbed = false
	delete(t.peersSnubbed, pe)
	return piecedownloader.New(pi.Piece, pe)
}

func (t *Torrent) findPieceAndPeer() (*piece.Piece, *peer.Peer) {
	pe, pi := t.select4RandomPiece()
	if pe != nil && pi != nil {
		return pe, pi
	}
	sort.Sort(piece.ByAvailability(t.sortedPieces))
	pe, pi = t.selectAllowedFastPiece(true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = t.selectUnchokedPeer(true, true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = t.selectSnubbedPeer(true)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = t.selectDuplicatePiece()
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}

func (t *Torrent) select4RandomPiece() (*piece.Piece, *peer.Peer) {
	// TODO request first 4 pieces randomly
	return nil, nil
}

func (t *Torrent) selectAllowedFastPiece(skipSnubbed, noDuplicate bool) (*piece.Piece, *peer.Peer) {
	for _, pi := range t.sortedPieces {
		if t.bitfield.Test(pi.Index) {
			continue
		}
		if noDuplicate && len(pi.RequestedPeers) > 0 {
			continue
		} else if pi.RunningDownloads() >= t.config.EndgameParallelDownloadsPerPiece {
			continue
		}
		for pe := range pi.HavingPeers {
			if _, ok := t.pieceDownloaders[pe]; ok {
				continue
			}
			if skipSnubbed && pe.Snubbed {
				continue
			}
			if _, ok := pe.AllowedFastPieces[pi.Index]; !ok {
				continue
			}
			return pi, pe
		}
	}
	return nil, nil
}

func (t *Torrent) selectUnchokedPeer(skipSnubbed, noDuplicate bool) (*piece.Piece, *peer.Peer) {
	for _, pi := range t.sortedPieces {
		if t.bitfield.Test(pi.Index) {
			continue
		}
		if noDuplicate && len(pi.RequestedPeers) > 0 {
			continue
		} else if pi.RunningDownloads() >= t.config.EndgameParallelDownloadsPerPiece {
			continue
		}
		for pe := range pi.HavingPeers {
			if _, ok := t.pieceDownloaders[pe]; ok {
				continue
			}
			if skipSnubbed && pe.Snubbed {
				continue
			}
			if pe.PeerChoking {
				continue
			}
			return pi, pe
		}
	}
	return nil, nil
}

func (t *Torrent) selectSnubbedPeer(noDuplicate bool) (*piece.Piece, *peer.Peer) {
	pi, pe := t.selectAllowedFastPiece(false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	pi, pe = t.selectUnchokedPeer(false, noDuplicate)
	if pi != nil && pe != nil {
		return pi, pe
	}
	return nil, nil
}

func (t *Torrent) selectDuplicatePiece() (*piece.Piece, *peer.Peer) {
	pe, pi := t.selectAllowedFastPiece(true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = t.selectUnchokedPeer(true, false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	pe, pi = t.selectSnubbedPeer(false)
	if pe != nil && pi != nil {
		return pe, pi
	}
	return nil, nil
}
