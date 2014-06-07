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
	Info InfoDict
	// InfoHash field does not exist in torrent file.
	// It is calculated when file is loaded from disk.
	InfoHash     [20]byte
	Announce     string
	AnnounceList [][]string `bencode:"announce-list"`
	CreationDate int64      `bencode:"creation date"`
	Comment      string
	CreatedBy    string `bencode:"created by"`
	Encoding     string
	// TotalLength field does not exist in torrent file.
	// It is calculated when file is loaded from disk.
	TotalLength int64
}

type InfoDict struct {
	PieceLength int64 `bencode:"piece length"`
	Pieces      string
	Private     int64
	Name        string
	// Single File Mode
	Length int64
	Md5sum string
	// Multiple File mode
	Files []FileDict
}

type FileDict struct {
	Length int64
	Path   []string
	Md5sum string
}

func LoadTorrentFile(path string) (*TorrentFile, error) {
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

	var infoBytes bytes.Buffer
	err = bencode.Marshal(&infoBytes, infoMap)
	if err != nil {
		return nil, err
	}

	t := &TorrentFile{}

	hash := sha1.New()
	hash.Write(infoBytes.Bytes())
	copy(t.InfoHash[:], hash.Sum(nil))

	reader.Seek(0, 0)
	err = bencode.Unmarshal(reader, t)
	if err != nil {
		return nil, err
	}

	t.TotalLength += t.Info.Length
	for _, f := range t.Info.Files {
		t.TotalLength += f.Length
	}

	return t, nil
}
