package torrent

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"os"

	"github.com/zeebo/bencode"

	"github.com/cenkalti/rain/bt"
)

type Torrent struct {
	Info         *Info              `bencode:"-"`
	RawInfo      bencode.RawMessage `bencode:"info" json:"-"`
	Announce     string             `bencode:"announce"`
	AnnounceList [][]string         `bencode:"announce-list"`
	CreationDate int64              `bencode:"creation date"`
	Comment      string             `bencode:"comment"`
	CreatedBy    string             `bencode:"created by"`
	Encoding     string             `bencode:"encoding"`
}

type Info struct {
	PieceLength uint32 `bencode:"piece length" json:"piece_length"`
	Pieces      []byte `bencode:"pieces" json:"pieces"`
	Private     byte   `bencode:"private" json:"private"`
	Name        string `bencode:"name" json:"name"`
	// Single File Mode
	Length int64  `bencode:"length" json:"length"`
	Md5sum string `bencode:"md5sum" json:"md5sum,omitempty"`
	// Multiple File mode
	Files []fileDict `bencode:"files" json:"files"`

	Raw []byte `bencode:"-" json:"-"`

	// Calculated fileds
	Hash        bt.InfoHash `bencode:"-" json:"-"`
	TotalLength int64             `bencode:"-" json:"-"`
	NumPieces   uint32            `bencode:"-" json:"-"`
	MultiFile   bool              `bencode:"-" json:"-"`
}

type fileDict struct {
	Length int64    `bencode:"length" json:"length"`
	Path   []string `bencode:"path" json:"path"`
	Md5sum string   `bencode:"md5sum" json:"md5sum,omitempty"`
}

func New(path string) (*Torrent, error) {
	var t Torrent

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	d := bencode.NewDecoder(f)
	err = d.Decode(&t)
	f.Close()
	if err != nil {
		return nil, err
	}

	if len(t.RawInfo) == 0 {
		return nil, errors.New("no info dict in torrent file")
	}

	t.Info, err = NewInfo(t.RawInfo)
	return &t, err
}

func NewInfo(b []byte) (*Info, error) {
	var i Info

	r := bytes.NewReader(b)
	d := bencode.NewDecoder(r)
	err := d.Decode(&i)
	if err != nil {
		return nil, err
	}

	i.Raw = append([]byte(nil), b...)

	hash := sha1.New()
	hash.Write(b)
	copy(i.Hash[:], hash.Sum(nil))

	i.MultiFile = len(i.Files) != 0

	i.NumPieces = uint32(len(i.Pieces)) / sha1.Size

	if !i.MultiFile {
		i.TotalLength = i.Length
	} else {
		for _, f := range i.Files {
			i.TotalLength += f.Length
		}
	}

	return &i, nil
}

func (i *Info) PieceHash(index uint32) []byte {
	if index >= i.NumPieces {
		panic("piece index out of range")
	}
	start := index * sha1.Size
	end := start + sha1.Size
	return i.Pieces[start:end]
}

// GetFiles returns the files in torrent as a slice, even if there is a single file.
func (i *Info) GetFiles() []fileDict {
	if i.MultiFile {
		return i.Files
	} else {
		return []fileDict{fileDict{i.Length, []string{i.Name}, i.Md5sum}}
	}
}
