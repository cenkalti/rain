package rain

import (
	"os"
	"path/filepath"
)

// transfer represents an active transfer in the program.
type transfer struct {
	torrentFile *TorrentFile
	where       string
	pieces      []*piece
	bitField    BitField // pieces we have
	// Stats
	Downloaded int64
	Uploaded   int64
}

func newTransfer(tor *TorrentFile, where string) *transfer {
	return &transfer{
		torrentFile: tor,
		where:       where,
		pieces:      newPieces(tor),
		bitField:    NewBitField(nil, tor.NumPieces),
	}
}

func newPieces(tor *TorrentFile) []*piece {
	var (
		fileIndex  int   // index of the current file in torrent
		fileLength int64 = tor.Info.Files[0].Length
		fileEnd    int64 = fileLength // absolute position of end of the file among all pieces
		fileOffset int64              // offset in file: [0, fileLength)
	)

	nextFile := func() {
		fileIndex++
		fileLength = tor.Info.Files[fileIndex].Length
		fileEnd += fileLength
		fileOffset = 0
	}

	// Construct pieces
	var total int64
	pieces := make([]*piece, tor.NumPieces)
	for i := int32(0); i < tor.NumPieces; i++ {
		p := &piece{
			index:  i,
			sha1:   tor.HashOfPiece(i),
			haveC:  make(chan *peerConn),
			pieceC: make(chan *peerPieceMessage),
		}

		// Construct p.targets
		var pieceOffset int32 = 0
		fileLeft := func() int64 { return fileLength - fileOffset }
		pieceLeft := func() int32 { return tor.Info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; nextFile() {
			n := int32(minInt64(int64(left), fileLeft())) // number of bytes to write

			target := &writeTarget{tor.Info.Files[fileIndex].file, fileOffset, n}
			p.targets = append(p.targets, target)

			left -= n

			p.length += n
			pieceOffset += n
			fileOffset += int64(n)

			total += int64(n)
			if total == tor.TotalLength {
				break
			}
		}

		// Construct p.blocks
		div, mod := divMod32(p.length, blockSize)
		numBlocks := div
		if mod != 0 {
			numBlocks++
		}
		p.bitField = NewBitField(nil, numBlocks)
		p.blocks = make([]*block, numBlocks)
		for j := int32(0); j < div; j++ {
			p.blocks[j] = &block{
				index:  j,
				length: blockSize,
			}
		}
		if mod != 0 {
			p.blocks[numBlocks-1] = &block{
				index:  numBlocks - 1,
				length: int32(mod),
			}
		}

		pieces[i] = p
	}
	return pieces
}

func (t *transfer) Left() int64 {
	// TODO return correct "left bytes"
	return t.torrentFile.TotalLength - t.Downloaded
}

func (t *transfer) allocate(where string) error {
	var err error
	info := &t.torrentFile.Info

	// Single file
	if info.Length != 0 {
		info.file, err = createTruncateSync(filepath.Join(where, info.Name), info.Length)
		return err
	}

	// Multiple files
	for _, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return err
		}
		f.file, err = createTruncateSync(path, f.Length)
		if err != nil {
			return err
		}
	}
	return nil
}

func createTruncateSync(path string, length int64) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	err = f.Truncate(length)
	if err != nil {
		return nil, err
	}

	err = f.Sync()
	if err != nil {
		return nil, err
	}

	return f, nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
