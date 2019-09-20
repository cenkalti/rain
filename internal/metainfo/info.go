package metainfo

import (
	"crypto/sha1" // nolint: gosec
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zeebo/bencode"
)

var (
	errInvalidPieceData = errors.New("invalid piece data")
	errZeroPieceLength  = errors.New("torrent has zero piece length")
	errZeroPieces       = errors.New("torrent has zero pieces")
)

// Info contains information about torrent.
type Info struct {
	PieceLength uint32
	Name        string
	Hash        [20]byte
	Length      int64
	NumPieces   uint32
	Bytes       []byte
	Private     bool
	Files       []File
	pieces      []byte
}

type File struct {
	Length int64
	Path   string
}

// NewInfo returns info from bencoded bytes in b.
func NewInfo(b []byte) (*Info, error) {
	type file struct {
		Length int64    `bencode:"length"`
		Path   []string `bencode:"path"`
	}
	var ib struct {
		PieceLength uint32             `bencode:"piece length"`
		Pieces      []byte             `bencode:"pieces"`
		Private     bencode.RawMessage `bencode:"private"`
		Name        string             `bencode:"name"`
		Length      int64              `bencode:"length"` // Single File Mode
		Files       []file             `bencode:"files"`  // Multiple File mode
	}
	if err := bencode.DecodeBytes(b, &ib); err != nil {
		return nil, err
	}
	if ib.PieceLength == 0 {
		return nil, errZeroPieceLength
	}
	if len(ib.Pieces)%sha1.Size != 0 {
		return nil, errInvalidPieceData
	}
	numPieces := len(ib.Pieces) / sha1.Size
	if numPieces == 0 {
		return nil, errZeroPieces
	}
	// ".." is not allowed in file names
	for _, file := range ib.Files {
		for _, path := range file.Path {
			if strings.TrimSpace(path) == ".." {
				return nil, fmt.Errorf("invalid file name: %q", filepath.Join(file.Path...))
			}
		}
	}
	i := Info{
		PieceLength: ib.PieceLength,
		NumPieces:   uint32(numPieces),
		pieces:      ib.Pieces,
		Name:        ib.Name,
		Private:     parsePrivateField(ib.Private),
	}
	multiFile := len(ib.Files) > 0
	if multiFile {
		for _, f := range ib.Files {
			i.Length += f.Length
		}
	} else {
		i.Length = ib.Length
	}
	totalPieceDataLength := int64(i.PieceLength) * int64(i.NumPieces)
	delta := totalPieceDataLength - i.Length
	if delta >= int64(i.PieceLength) || delta < 0 {
		return nil, errInvalidPieceData
	}
	i.Bytes = b

	// calculate info hash
	hash := sha1.New()   // nolint: gosec
	_, _ = hash.Write(b) // nolint: gosec
	copy(i.Hash[:], hash.Sum(nil))

	// name field is optional
	if ib.Name != "" {
		i.Name = ib.Name
	} else {
		i.Name = hex.EncodeToString(i.Hash[:])
	}

	// construct files
	if multiFile {
		i.Files = make([]File, len(ib.Files))
		for j, f := range ib.Files {
			i.Files[j] = File{
				Path:   filepath.Join(i.Name, filepath.Join(f.Path...)),
				Length: f.Length,
			}
		}
	} else {
		i.Files = []File{{Path: i.Name, Length: i.Length}}
	}
	return &i, nil
}

func parsePrivateField(s bencode.RawMessage) bool {
	if len(s) == 0 {
		return false
	}
	var intVal int64
	err := bencode.DecodeBytes(s, &intVal)
	if err == nil {
		return intVal == 1
	}
	var stringVal string
	err = bencode.DecodeBytes(s, &stringVal)
	if err == nil {
		return stringVal == "1"
	}
	return false
}

func (i *Info) PieceHash(index uint32) []byte {
	begin := index * sha1.Size
	end := begin + sha1.Size
	return i.pieces[begin:end]
}
