package rain

import (
	"crypto/sha1"
	"os"
	"strconv"
)

type piece struct {
	index             int32 // piece index in whole torrent
	sha1              [sha1.Size]byte
	length            int32        // last piece may not be complete
	files             partialFiles // the place to write downloaded bytes
	blocks            []block
	bitField          bitField       // blocks we have
	haveC             chan *peerConn // message sent to here when a peer has this piece
	peerDisconnectedC chan *peerConn // message sent to here when a peer disconnects
	pieceC            chan []byte
	peers             []*peerConn // contains peers that have this piece
	downloaded        chan struct{}
	log               logger
}

type block struct {
	index  int32 // block index in piece
	length int32
	files  partialFiles // the place to write downloaded bytes
}

func newPieces(info *infoDict, osFiles []*os.File) []*piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 = info.GetFiles()[0].Length
		fileEnd    int64 = fileLength // absolute position of end of the file among all pieces
		fileOffset int64              // offset in file: [0, fileLength)
	)

	nextFile := func() {
		fileIndex++
		fileLength = info.GetFiles()[fileIndex].Length
		fileEnd += fileLength
		fileOffset = 0
	}
	fileLeft := func() int64 { return fileLength - fileOffset }

	// Construct pieces
	var total int64
	pieces := make([]*piece, info.NumPieces)
	for i := int32(0); i < info.NumPieces; i++ {
		p := &piece{
			index:             i,
			sha1:              info.HashOfPiece(i),
			haveC:             make(chan *peerConn),
			peerDisconnectedC: make(chan *peerConn),
			pieceC:            make(chan []byte),
			downloaded:        make(chan struct{}),
			log:               newLogger("piece #" + strconv.Itoa(int(i))),
		}

		// Construct p.files
		var pieceOffset int32
		pieceLeft := func() int32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := int32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := partialFile{osFiles[fileIndex], fileOffset, n}
			p.log.Debugf("file: %#v", file)
			p.files = append(p.files, file)

			left -= n
			p.length += n
			pieceOffset += n
			fileOffset += int64(n)
			total += int64(n)

			if total == info.TotalLength {
				break
			}
			if fileLeft() == 0 {
				nextFile()
			}
		}

		p.blocks = newBlocks(p.length, p.files)
		p.bitField = newBitField(nil, int32(len(p.blocks)))
		pieces[i] = p
	}
	return pieces
}

func newBlocks(pieceLength int32, files []partialFile) []block {
	div, mod := divMod32(pieceLength, blockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for j := int32(0); j < div; j++ {
		blocks[j] = block{
			index:  j,
			length: blockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = block{
			index:  numBlocks - 1,
			length: int32(mod),
		}
	}
	var fileIndex int
	var fileOffset int32
	nextFile := func() {
		fileIndex++
		fileOffset = 0
	}
	fileLeft := func() int32 { return files[fileIndex].length - fileOffset }
	for i := range blocks {
		var blockOffset int32 = 0
		blockLeft := func() int32 { return blocks[i].length - blockOffset }
		for left := blockLeft(); left > 0 && fileIndex < len(files); {
			n := minInt32(left, fileLeft())
			file := partialFile{files[fileIndex].file, files[fileIndex].offset + int64(fileOffset), n}
			blocks[i].files = append(blocks[i].files, file)
			fileOffset += n
			blockOffset += n
			left -= n
			if fileLeft() == 0 {
				nextFile()
			}
		}
	}
	return blocks
}

func (p *piece) run() {
	for {
		select {
		case peer := <-p.haveC:
			p.peers = append(p.peers, peer)
			go func(peer *peerConn) {
				<-peer.disconnect
				p.peerDisconnectedC <- peer
			}(peer)
			// case peer := <-p.peerDisconnectedC:
			// TODO remove from p.peers
		}
	}
}
