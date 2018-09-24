package metainfo

import (
	"crypto/sha1" // nolint: gosec

	"github.com/zeebo/bencode"
)

// Info contains information about torrent.
type Info struct {
	PieceLength uint32     `bencode:"piece length" json:"piece_length"`
	Pieces      []byte     `bencode:"pieces" json:"pieces"`
	Private     byte       `bencode:"private" json:"private"`
	Name        string     `bencode:"name" json:"name"`
	Length      int64      `bencode:"length" json:"length"` // Single File Mode
	Files       []FileDict `bencode:"files" json:"files"`   // Multiple File mode

	// Calculated fileds
	Hash        [20]byte `bencode:"-" json:"-"`
	PieceHashes [][]byte `bencode:"-" json:"-"`
	TotalLength int64    `bencode:"-" json:"-"`
	NumPieces   uint32   `bencode:"-" json:"-"`
	MultiFile   bool     `bencode:"-" json:"-"`
	InfoSize    uint32   `bencode:"-" json:"-"`
	Bytes       []byte   `bencode:"-" json:"-"`
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
	hash := sha1.New() // nolint: gosec
	hash.Write(b)      // nolint: gosec
	copy(i.Hash[:], hash.Sum(nil))
	i.NumPieces = uint32(len(i.Pieces)) / sha1.Size
	i.MultiFile = len(i.Files) != 0
	if !i.MultiFile {
		i.TotalLength = i.Length
	} else {
		for _, f := range i.Files {
			i.TotalLength += f.Length
		}
	}
	i.PieceHashes = make([][]byte, i.NumPieces)
	for idx := uint32(0); idx < i.NumPieces; idx++ {
		begin := idx * sha1.Size
		end := begin + sha1.Size
		i.PieceHashes[idx] = i.Pieces[begin:end]
	}
	i.InfoSize = uint32(len(b))
	i.Bytes = b
	return &i, nil
}

// GetFiles returns the files in torrent as a slice, even if there is a single file.
func (i *Info) GetFiles() []FileDict {
	if i.MultiFile {
		return i.Files
	}
	return []FileDict{FileDict{i.Length, []string{i.Name}}}
}
