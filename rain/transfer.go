package rain

import (
	"os"
	"path/filepath"
	"strconv"
)

// transfer represents an active transfer in the program.
type transfer struct {
	torrentFile *torrentFile
	pieces      []*piece
	bitField    bitField // pieces we have
	// Stats
	Downloaded int64
	Uploaded   int64
	log        logger
}

func newTransfer(tor *torrentFile, where string) (*transfer, error) {
	files, err := allocate(&tor.Info, where)
	if err != nil {
		return nil, err
	}
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	t := &transfer{
		torrentFile: tor,
		pieces:      newPieces(&tor.Info, files),
		bitField:    newBitField(nil, tor.Info.NumPieces),
		log:         newLogger("download " + name),
	}
	return t, nil
}

func newPieces(info *infoDict, files []*os.File) []*piece {
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

	// Construct pieces
	var total int64
	pieces := make([]*piece, info.NumPieces)
	for i := int32(0); i < info.NumPieces; i++ {
		p := &piece{
			index:  i,
			sha1:   info.HashOfPiece(i),
			haveC:  make(chan *peerConn),
			pieceC: make(chan *peerPieceMessage),
			log:    newLogger("piece #" + strconv.Itoa(int(i))),
		}

		// Construct p.targets
		var pieceOffset int32 = 0
		fileLeft := func() int64 { return fileLength - fileOffset }
		pieceLeft := func() int32 { return info.PieceLength - pieceOffset }
		for left := pieceLeft(); left > 0; nextFile() {
			n := int32(minInt64(int64(left), fileLeft())) // number of bytes to write

			target := &writeTarget{files[fileIndex], fileOffset, n}
			p.targets = append(p.targets, target)

			left -= n
			p.length += n
			pieceOffset += n
			fileOffset += int64(n)
			total += int64(n)

			if total == info.TotalLength {
				break
			}
		}
		p.blocks = newBlocks(p.length)
		p.bitField = newBitField(nil, int32(len(p.blocks)))
		pieces[i] = p
	}
	return pieces
}

func newBlocks(pieceLength int32) []*block {
	div, mod := divMod32(pieceLength, blockSize)
	numBlocks := div
	if mod != 0 {
		numBlocks++
	}
	blocks := make([]*block, numBlocks)
	for j := int32(0); j < div; j++ {
		blocks[j] = &block{
			index:  j,
			length: blockSize,
		}
	}
	if mod != 0 {
		blocks[numBlocks-1] = &block{
			index:  numBlocks - 1,
			length: int32(mod),
		}
	}
	return blocks
}

func (t *transfer) Left() int64 {
	// TODO return correct "left bytes"
	return t.torrentFile.Info.TotalLength - t.Downloaded
}

func allocate(info *infoDict, where string) ([]*os.File, error) {
	if !info.MultiFile() {
		f, err := createTruncateSync(filepath.Join(where, info.Name), info.Length)
		if err != nil {
			return nil, err
		}
		return []*os.File{f}, nil
	}

	// Multiple files
	files := make([]*os.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err := os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return nil, err
		}
		files[i], err = createTruncateSync(path, f.Length)
		if err != nil {
			return nil, err
		}
	}
	return files, nil
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
