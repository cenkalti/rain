package metainfo

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/zeebo/bencode"
)

var (
	errInvalidPieceData = errors.New("invalid piece data")
	errZeroPieceLength  = errors.New("torrent has zero piece length")
	errZeroPieces       = errors.New("torrent has zero pieces")
	errPieceLength      = errors.New("piece length must be multiple of 16K")
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

// File represents a file inside a Torrent.
type File struct {
	Length int64
	Path   string
	// https://www.bittorrent.org/beps/bep_0047.html
	Padding bool
}

type file struct {
	Length   int64    `bencode:"length"`
	Path     []string `bencode:"path"`
	PathUTF8 []string `bencode:"path.utf-8,omitempty"`
	Attr     string   `bencode:"attr"`
}

func (f *file) isPadding() bool {
	// BEP 0047
	if strings.ContainsRune(f.Attr, 'p') {
		return true
	}
	// BitComet convention that do not conform BEP 0047
	if len(f.Path) > 0 && strings.HasPrefix(f.Path[len(f.Path)-1], "_____padding_file") {
		return true
	}
	return false
}

type infoType struct {
	PieceLength uint32             `bencode:"piece length"`
	Pieces      []byte             `bencode:"pieces"`
	Name        string             `bencode:"name"`
	NameUTF8    string             `bencode:"name.utf-8,omitempty"`
	Private     bencode.RawMessage `bencode:"private"`
	Length      int64              `bencode:"length"` // Single File Mode
	Files       []file             `bencode:"files"`  // Multiple File mode
}

func (ib *infoType) overrideUTF8Keys() {
	if len(ib.NameUTF8) > 0 {
		ib.Name = ib.NameUTF8
	}
	for i := range ib.Files {
		if len(ib.Files[i].PathUTF8) > 0 {
			ib.Files[i].Path = ib.Files[i].PathUTF8
		}
	}
}

// NewInfo returns info from bencoded bytes in b.
func NewInfo(b []byte, utf8 bool, pad bool) (*Info, error) {
	var ib infoType
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
	if utf8 {
		ib.overrideUTF8Keys()
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
	hash := sha1.New()
	_, _ = hash.Write(b)
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
		uniquePaths := make(map[string]interface{}, len(ib.Files))
		for j, f := range ib.Files {
			parts := make([]string, 0, len(f.Path)+1)
			parts = append(parts, cleanName(i.Name))
			for _, p := range f.Path {
				parts = append(parts, cleanName(p))
			}
			joinedPath := filepath.Join(parts...)
			if _, ok := uniquePaths[joinedPath]; ok {
				return nil, fmt.Errorf("duplicate file name: %q", joinedPath)
			} else {
				uniquePaths[joinedPath] = nil
			}
			i.Files[j] = File{
				Path:   joinedPath,
				Length: f.Length,
			}
			if pad {
				i.Files[j].Padding = f.isPadding()
			}
		}
	} else {
		i.Files = []File{{Path: cleanName(i.Name), Length: i.Length}}
	}
	return &i, nil
}

func cleanName(s string) string {
	return cleanNameN(s, 255)
}

func cleanNameN(s string, max int) string {
	s = strings.ToValidUTF8(s, string(unicode.ReplacementChar))
	s = trimName(s, max)
	s = strings.ToValidUTF8(s, "")
	return replaceSeparator(s)
}

// trimName trims the file name that it won't exceed 255 characters while keeping the extension.
func trimName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	ext := path.Ext(s)
	if len(ext) > max {
		return s[:max]
	}
	return s[:max-len(ext)] + ext
}

func replaceSeparator(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' {
			return '_'
		}
		return r
	}, s)
}

func parsePrivateField(s bencode.RawMessage) bool {
	if len(s) == 0 {
		return false
	}
	var intVal int64
	err := bencode.DecodeBytes(s, &intVal)
	if err == nil {
		return intVal != 0
	}
	var stringVal string
	err = bencode.DecodeBytes(s, &stringVal)
	if err != nil {
		return true
	}
	return !(stringVal == "" || stringVal == "0")
}

// NewInfoBytes creates a new Info dictionary by reading and hashing the files on the disk.
func NewInfoBytes(root string, paths []string, private bool, pieceLength uint32, name string, log logger.Logger) ([]byte, error) {
	var singleFileTorrent bool
	switch len(paths) {
	case 0:
		return nil, errors.New("no path specified")
	case 1:
		if name == "" {
			name = filepath.Base(paths[0])
		}
		fi, err := os.Stat(paths[0])
		if err != nil {
			return nil, err
		}
		singleFileTorrent = !fi.IsDir()
	default:
		if root == "" {
			return nil, errors.New("no root specified")
		}
		if name == "" {
			return nil, errors.New("no name specified")
		}
	}
	totalLength, err := findTotalLength(paths)
	if err != nil {
		return nil, err
	}
	if totalLength == 0 {
		return nil, errors.New("no files")
	}
	if pieceLength == 0 {
		pieceLength = calculatePieceLength(totalLength)
		log.Infof("Calculated piece length: %d K", pieceLength>>10)
	} else if pieceLength%(16<<10) != 0 {
		return nil, errPieceLength
	}
	buf := make([]byte, pieceLength)
	offset := 0
	remaining := func() []byte { return buf[offset:] }
	hash := sha1.New()
	var files []file
	var pieces []byte
	for _, path := range paths {
		relroot := path
		if root != "" {
			relroot = root
		}
		visit := func(vpath string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			f, err := os.Open(vpath)
			if err != nil {
				return err
			}
			defer f.Close()
			relpath, err := filepath.Rel(relroot, vpath)
			log.Infof("Adding %q", relpath)
			if err != nil {
				return err
			}
			files = append(files, file{Path: strings.Split(relpath, string(os.PathSeparator)), Length: fi.Size()})
			for {
				n, err := io.ReadFull(f, remaining())
				offset += n
				if err == io.ErrUnexpectedEOF || err == io.EOF {
					return nil // file finished, continue with next file
				}
				if err != nil {
					return err
				}
				// buffer finished, calculate piece hash and append to pieces
				_, _ = hash.Write(buf)
				pieces = hash.Sum(pieces)
				hash.Reset()
				offset = 0
			}
		}
		err = filepath.Walk(path, visit)
		if err != nil {
			return nil, err
		}
	}
	// hash remaining buffer
	if offset > 0 {
		_, _ = hash.Write(buf[:offset])
		pieces = hash.Sum(pieces)
	}
	b := struct {
		Name        string `bencode:"name"`
		Private     bool   `bencode:"private"`
		PieceLength uint32 `bencode:"piece length"`
		Pieces      []byte `bencode:"pieces"`
		Length      int64  `bencode:"length,omitempty"` // Single File Mode
		Files       []file `bencode:"files,omitempty"`  // Multiple File mode
	}{
		Name:        name,
		Private:     private,
		PieceLength: pieceLength,
		Pieces:      pieces,
	}
	if singleFileTorrent {
		b.Length = totalLength
	} else {
		b.Files = files
	}
	return bencode.EncodeBytes(b)
}

// PieceHash returns the hash of a piece at index.
func (i *Info) PieceHash(index uint32) []byte {
	begin := index * sha1.Size
	end := begin + sha1.Size
	return i.pieces[begin:end]
}

func findTotalLength(paths []string) (n int64, err error) {
	for _, path := range paths {
		err = filepath.Walk(path, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			n += fi.Size()
			return nil
		})
		if err != nil {
			return
		}
	}
	return
}

func calculatePieceLength(totalLength int64) uint32 {
	const maxPieces = 2000
	pieceLength := totalLength / maxPieces
	switch {
	case pieceLength < 32<<10:
		return 32 << 10
	case pieceLength > 16<<20:
		return 16 << 20
	default:
		// round to next power of 2
		v := uint32(pieceLength)
		v--
		v |= v >> 1
		v |= v >> 2
		v |= v >> 4
		v |= v >> 8
		v |= v >> 16
		v++
		return v
	}
}
