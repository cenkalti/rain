package rain

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io/ioutil"
	"os"

	"code.google.com/p/bencode-go"
)

type TorrentFile struct {
	Info         infoDict
	Announce     string
	AnnounceList [][]string `bencode:"announce-list"`
	CreationDate int64      `bencode:"creation date"`
	Comment      string
	CreatedBy    string `bencode:"created by"`
	Encoding     string

	// These fields do not exist in torrent file.
	// They are calculated when a TorrentFile is created with NewTorrentFile func.
	InfoHash    infoHash
	TotalLength int64
	NumPieces   int32
}

type infoHash [sha1.Size]byte

type infoDict struct {
	PieceLength int32 `bencode:"piece length"`
	Pieces      string
	Private     byte
	Name        string
	// Single File Mode
	Length int64
	Md5sum string
	file   *os.File
	// Multiple File mode
	Files []fileDict
}

type fileDict struct {
	Length int64
	Path   []string
	Md5sum string
	file   *os.File
}

func NewTorrentFile(path string) (*TorrentFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	data, err := ioutil.ReadAll(file)
	file.Close()
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(data)

	decoded, err := bencode.Decode(reader)
	if err != nil {
		return nil, err
	}

	torrentMap, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, errors.New("invalid torrent file")
	}

	infoMap, ok := torrentMap["info"]
	if !ok {
		return nil, errors.New("invalid torrent file")
	}

	t := new(TorrentFile)

	// Unmarshal bencoded bytes into the struct
	reader.Seek(0, 0)
	err = bencode.Unmarshal(reader, t)
	if err != nil {
		return nil, err
	}

	// Calculate InfoHash
	hash := sha1.New()
	bencode.Marshal(hash, infoMap)
	copy(t.InfoHash[:], hash.Sum(nil))

	// Calculate TotalLength
	t.TotalLength += t.Info.Length
	for _, f := range t.Info.Files {
		t.TotalLength += f.Length
	}

	// Calculate NumPieces
	t.NumPieces = int32(len(t.Info.Pieces)) / sha1.Size

	return t, nil
}

func (t *TorrentFile) HashOfPiece(i int32) [sha1.Size]byte {
	var hash [sha1.Size]byte
	start := i * sha1.Size
	end := start + sha1.Size
	copy(hash[:], []byte(t.Info.Pieces[start:end]))
	return hash
}
