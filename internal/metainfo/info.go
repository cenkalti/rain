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
	PieceLength uint32             `bencode:"piece length"`
	Pieces      []byte             `bencode:"pieces"`
	Private     bencode.RawMessage `bencode:"private"`
	Name        string             `bencode:"name"`
	Length      int64              `bencode:"length"` // Single File Mode
	Files       []File             `bencode:"files"`  // Multiple File mode

	hash        [20]byte
	totalLength int64
	numPieces   uint32
	bytes       []byte
	private     bool
}

type File struct {
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"`
}

// NewInfo returns info from bencoded bytes in b.
func NewInfo(b []byte) (*Info, error) {
	var i Info
	if err := bencode.DecodeBytes(b, &i); err != nil {
		return nil, err
	}
	if uint32(len(i.Pieces))%sha1.Size != 0 {
		return nil, errInvalidPieceData
	}
	if len(i.Private) > 0 {
		var intVal int64
		var stringVal string
		err := bencode.DecodeBytes(i.Private, &intVal)
		if err != nil {
			err = bencode.DecodeBytes(i.Private, &stringVal)
			if err == nil {
				i.private = stringVal == "1"
			}
		} else {
			i.private = intVal == 1
		}
	}
	// ".." is not allowed in file names
	for _, file := range i.Files {
		for _, path := range file.Path {
			if strings.TrimSpace(path) == ".." {
				return nil, fmt.Errorf("invalid file name: %q", filepath.Join(file.Path...))
			}
		}
	}
	i.numPieces = uint32(len(i.Pieces)) / sha1.Size
	if !i.MultiFile() {
		i.totalLength = i.Length
	} else {
		for _, f := range i.Files {
			i.totalLength += f.Length
		}
	}
	totalPieceDataLength := int64(i.PieceLength) * int64(i.numPieces)
	delta := totalPieceDataLength - i.totalLength
	if delta >= int64(i.PieceLength) || delta < 0 {
		return nil, errInvalidPieceData
	}
	i.bytes = b
	hash := sha1.New()   // nolint: gosec
	_, _ = hash.Write(b) // nolint: gosec
	copy(i.hash[:], hash.Sum(nil))
	return &i, nil
}

func (i *Info) MultiFile() bool {
	return len(i.Files) != 0
}

func (i *Info) PieceHash(index uint32) []byte {
	begin := index * sha1.Size
	end := begin + sha1.Size
	return i.Pieces[begin:end]
}

// GetFiles returns the files in torrent as a slice, even if there is a single file.
func (i *Info) GetFiles() []File {
	if i.MultiFile() {
		return i.Files
	}
	return []File{{i.Length, []string{i.Name}}}
}

func (i *Info) Hash() []byte {
	return i.hash[:]
}

func (i *Info) TotalLength() int64 {
	return i.totalLength
}

func (i *Info) NumPieces() uint32 {
	return i.numPieces
}

func (i *Info) Bytes() []byte {
	return i.bytes
}

func (i *Info) IsPrivate() bool {
	if i == nil {
		return false
	}
	return i.private
}
