package rain

import (
	"crypto/sha1"
	"os"
	"strconv"
	"sync"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/partialfile"
	"github.com/cenkalti/rain/internal/torrent"
)

type piece struct {
	index    uint32 // piece index in whole torrent
	sha1     [sha1.Size]byte
	length   uint32            // last piece may not be complete
	files    partialfile.Files // the place to write downloaded bytes
	blocks   []block
	bitField bitfield.BitField // blocks we have
	peers    []*peerConn       // contains peers that have this piece
	peersM   sync.Mutex
	ok       bool // we have the piece and hash check is ok
	blockC   chan peerBlock
	log      logger.Logger
}

type block struct {
	index  uint32 // block index in piece
	length uint32
	files  partialfile.Files // the place to write downloaded bytes
}

func newPieces(info *torrent.Info, osFiles []*os.File) []*piece {
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
	for i := uint32(0); i < info.NumPieces; i++ {
		p := &piece{
			index:  i,
			sha1:   info.HashOfPiece(i),
			blockC: make(chan peerBlock),
			log:    logger.New("piece #" + strconv.Itoa(int(i))),
		}

		// Construct p.files
		var pieceOffset uint32
		pieceLeft := func() uint32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; {
			n := uint32(minInt64(int64(left), fileLeft())) // number of bytes to write

			file := partialfile.File{osFiles[fileIndex], fileOffset, n}
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
		p.bitField = bitfield.New(nil, uint32(len(p.blocks)))
		pieces[i] = p
	}
	return pieces
}

func newBlocks(pieceLength uint32, files partialfile.Files) []block {
	div, mod := divMod32(pieceLength, blockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]block, numBlocks)
	for j := uint32(0); j < div; j++ {
		blocks[j] = block{
			index:  j,
			length: blockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = block{
			index:  numBlocks - 1,
			length: uint32(mod),
		}
	}
	var fileIndex int
	var fileOffset uint32
	nextFile := func() {
		fileIndex++
		fileOffset = 0
	}
	fileLeft := func() uint32 { return files[fileIndex].Length - fileOffset }
	for i := range blocks {
		var blockOffset uint32 = 0
		blockLeft := func() uint32 { return blocks[i].length - blockOffset }
		for left := blockLeft(); left > 0 && fileIndex < len(files); {
			n := minUint32(left, fileLeft())
			file := partialfile.File{files[fileIndex].File, files[fileIndex].Offset + int64(fileOffset), n}
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

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
