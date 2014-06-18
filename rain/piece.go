package rain

import (
	"bytes"
	"crypto/sha1"
	"time"
)

type piece struct {
	index      int32 // piece index in whole torrent
	sha1       [sha1.Size]byte
	length     int32        // last piece may not be complete
	files      partialFiles // the place to write downloaded bytes
	blocks     []block
	bitField   bitField // blocks we have
	haveC      chan *peerConn
	pieceC     chan *peerPieceMessage
	downloaded bool
	log        logger
}

type block struct {
	index  int32 // block index in piece
	length int32
	files  partialFiles // the place to write downloaded bytes
}

func (p *piece) run() {
	for {
		select {
		case peer := <-p.haveC:
			if p.downloaded {
				p.log.Debug("Piece is already downloaded")
				break
			}

			unchokeC, err := peer.beInterested()
			if err != nil {
				p.log.Error(err)
				break
			}

			select {
			case <-unchokeC:
				pieceData := make([]byte, p.length)
				for i, b := range p.blocks {
					if err := peer.sendRequest(newPeerRequestMessage(p.index, b.index*blockSize, b.length)); err != nil {
						p.log.Error(err)
						break
					}
					select {
					case piece := <-p.pieceC:
						p.log.Noticeln("received piece of length", len(piece.Block))
						copy(pieceData[piece.Begin:], piece.Block)
						if _, err = b.files.Write(piece.Block); err != nil {
							p.log.Error(err)
							break
						}
						p.bitField.Set(int32(i))
					case <-time.After(time.Minute):
						p.log.Infof("Peer did not send piece #%d block #%d", p.index, b.index)
					}
				}

				// Verify piece hash
				hash := sha1.New()
				hash.Write(pieceData)
				if bytes.Compare(hash.Sum(nil), p.sha1[:]) != 0 {
					p.log.Error("received corrupt piece")
					return
				}

				p.log.Debug("piece written successfully")
			case <-time.After(time.Minute):
				p.log.Info("Peer did not unchoke")
			}
		}
	}
}
