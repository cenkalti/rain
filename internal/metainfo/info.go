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
	PieceLength uint32             `bencode:"piece length" json:"piece_length"`
	Pieces      []byte             `bencode:"pieces" json:"pieces"`
	Private     bencode.RawMessage `bencode:"private" json:"private"`
	Name        string             `bencode:"name" json:"name"`
	Length      int64              `bencode:"length" json:"length"` // Single File Mode
	Files       []FileDict         `bencode:"files" json:"files"`   // Multiple File mode

	// Calculated fileds
	Hash        [20]byte `bencode:"-" json:"-"`
	TotalLength int64    `bencode:"-" json:"-"`
	NumPieces   uint32   `bencode:"-" json:"-"`
	Bytes       []byte   `bencode:"-" json:"-"`
	private     bool
}

type FileDict struct {
	Length int64    `bencode:"length" json:"length"`
	Path   []string `bencode:"path" json:"path"`
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
	i.NumPieces = uint32(len(i.Pieces)) / sha1.Size
	if !i.MultiFile() {
		i.TotalLength = i.Length
	} else {
		for _, f := range i.Files {
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
	return len(i.Files) != 0
}

func (i *Info) HashOf(index uint32) []byte {
	begin := index * sha1.Size
	end := begin + sha1.Size
	return i.Pieces[begin:end]
}

// GetFiles returns the files in torrent as a slice, even if there is a single file.
func (i *Info) GetFiles() []FileDict {
	if i.MultiFile() {
		return i.Files
	}
	return []FileDict{{i.Length, []string{i.Name}}}
}

func (i *Info) IsPrivate() bool {
	if i == nil {
		return false
	}
	return i.private
}
