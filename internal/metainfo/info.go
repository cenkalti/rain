package metainfo

import (
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zeebo/bencode"
)

var errInvalidPieceData = errors.New("invalid piece data")

// Info contains information about torrent.
type Info struct {
	PieceLength uint32
	Name        string
	Length      int64
	Hash        [20]byte
	TotalLength int64
	NumPieces   uint32
	Bytes       []byte

	private bool
	pieces  []byte
	files   []File
}

type File struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

// NewInfo returns info from bencoded bytes in b.
func NewInfo(b []byte) (*Info, error) {
	var ib struct {
		PieceLength uint32             `bencode:"piece length"`
		Pieces      []byte             `bencode:"pieces"`
		Private     bencode.RawMessage `bencode:"private"`
		Name        string             `bencode:"name"`
		Length      int64              `bencode:"length"` // Single File Mode
		Files       []File             `bencode:"files"`  // Multiple File mode
	}

	if err := bencode.DecodeBytes(b, &ib); err != nil {
		return nil, err
	}
	i := Info{
		PieceLength: ib.PieceLength,
		pieces:      ib.Pieces,
		Name:        ib.Name,
		Length:      ib.Length,
		files:       ib.Files,
	}
	if uint32(len(i.pieces))%sha1.Size != 0 {
		return nil, errInvalidPieceData
	}
	if len(ib.Private) > 0 {
		var intVal int64
		var stringVal string
		err := bencode.DecodeBytes(ib.Private, &intVal)
		if err != nil {
			err = bencode.DecodeBytes(ib.Private, &stringVal)
			if err == nil {
				i.private = stringVal == "1"
			}
		} else {
			i.private = intVal == 1
		}
	}
	// ".." is not allowed in file names
	for _, file := range i.files {
		for _, path := range file.Path {
			if strings.TrimSpace(path) == ".." {
				return nil, fmt.Errorf("invalid file name: %q", filepath.Join(file.Path...))
			}
		}
	}
	i.NumPieces = uint32(len(i.pieces)) / sha1.Size
	if !i.MultiFile() {
		i.TotalLength = i.Length
	} else {
		for _, f := range i.files {
			i.TotalLength += f.Length
		}
	}
	totalPieceDataLength := int64(i.PieceLength) * int64(i.NumPieces)
	delta := totalPieceDataLength - i.TotalLength
	if delta >= int64(i.PieceLength) || delta < 0 {
		return nil, errInvalidPieceData
	}
	i.Bytes = b
	hash := sha1.New()   // nolint: gosec
	_, _ = hash.Write(b) // nolint: gosec
	copy(i.Hash[:], hash.Sum(nil))
	return &i, nil
}

func (i *Info) MultiFile() bool {
	return len(i.files) != 0
}

func (i *Info) PieceHash(index uint32) []byte {
	begin := index * sha1.Size
	end := begin + sha1.Size
	return i.pieces[begin:end]
}

// GetFiles returns the files in torrent as a slice, even if there is a single file.
func (i *Info) GetFiles() []File {
	if i.MultiFile() {
		return i.files
	}
	return []File{{i.Length, []string{i.Name}}}
}

func (i *Info) IsPrivate() bool {
	if i == nil {
		return false
	}
	return i.private
}
