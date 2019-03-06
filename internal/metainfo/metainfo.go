// Package metainfo support for reading and writing torrent files.
package metainfo

import (
	"errors"
	"io"

	"github.com/zeebo/bencode"
)

// MetaInfo file dictionary
type MetaInfo struct {
	Info         *Info              `bencode:"-"`
	RawInfo      bencode.RawMessage `bencode:"info" json:"-"`
	Announce     string             `bencode:"announce"`
	AnnounceList [][]string         `bencode:"announce-list"`
	CreationDate int64              `bencode:"creation date"`
	Comment      string             `bencode:"comment"`
	CreatedBy    string             `bencode:"created by"`
	Encoding     string             `bencode:"encoding"`
	URLList      URLList            `bencode:"url-list"`
}

// New returns a torrent from bencoded stream.
func New(r io.Reader) (*MetaInfo, error) {
	var t MetaInfo
	err := bencode.NewDecoder(r).Decode(&t)
	if err != nil {
		return nil, err
	}
	if len(t.RawInfo) == 0 {
		return nil, errors.New("no info dict in torrent file")
	}
	t.Info, err = NewInfo(t.RawInfo)
	return &t, err
}

func (m *MetaInfo) GetTrackers() []string {
	var trackers []string
	if len(m.AnnounceList) > 0 {
		for _, t := range m.AnnounceList {
			if len(t) > 0 {
				trackers = append(trackers, t[0])
			}
		}
	} else {
		trackers = []string{m.Announce}
	}
	return trackers
}
