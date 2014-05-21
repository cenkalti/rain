package main

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io/ioutil"
	"os"

	"code.google.com/p/bencode-go"
)

type TorrentFile struct {
	Info         InfoDict
	InfoHash     string
	Announce     string
	AnnounceList [][]string "announce-list"
	CreationDate int64      "creation date"
	Comment      string
	CreatedBy    string "created by"
	Encoding     string
}

func (m *TorrentFile) Load(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}

	data, err := ioutil.ReadAll(file)
	file.Close()
	if err != nil {
		return err
	}

	reader := bytes.NewReader(data)

	decoded, err := bencode.Decode(reader)
	if err != nil {
		return err
	}

	torrentMap, ok := decoded.(map[string]interface{})
	if !ok {
		return errors.New("invalid torrent file")
	}

	infoMap, ok := torrentMap["info"]
	if !ok {
		return errors.New("invalid torrent file")
	}

	var infoBytes bytes.Buffer
	err = bencode.Marshal(&infoBytes, infoMap)
	if err != nil {
		return err
	}

	hash := sha1.New()
	hash.Write(infoBytes.Bytes())
	m.InfoHash = string(hash.Sum(nil))

	reader.Seek(0, 0)
	return bencode.Unmarshal(reader, m)
}

type InfoDict struct {
	PieceLength int64 "piece length"
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
