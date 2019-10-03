package boltdbresumer

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

// Spec contains fields for resuming an existing torrent.
type Spec struct {
	InfoHash          []byte
	Port              int
	Name              string
	Trackers          [][]string
	URLList           []string
	FixedPeers        []string
	Info              []byte
	Bitfield          []byte
	AddedAt           time.Time
	BytesDownloaded   int64
	BytesUploaded     int64
	BytesWasted       int64
	SeededFor         time.Duration
	Started           bool
	StopAfterDownload bool
}

type jsonSpec struct {
	Port              int
	Name              string
	Trackers          [][]string
	URLList           []string
	FixedPeers        []string
	AddedAt           time.Time
	BytesDownloaded   int64
	BytesUploaded     int64
	BytesWasted       int64
	Started           bool
	StopAfterDownload bool

	// JSON safe types
	InfoHash  string
	Info      string
	Bitfield  string
	SeededFor int64
}

// MarshalJSON converts the Spec to a JSON string.
func (s Spec) MarshalJSON() ([]byte, error) {
	j := jsonSpec{
		Port:              s.Port,
		Name:              s.Name,
		Trackers:          s.Trackers,
		URLList:           s.URLList,
		FixedPeers:        s.FixedPeers,
		AddedAt:           s.AddedAt,
		BytesDownloaded:   s.BytesDownloaded,
		BytesUploaded:     s.BytesUploaded,
		BytesWasted:       s.BytesWasted,
		Started:           s.Started,
		StopAfterDownload: s.StopAfterDownload,

		InfoHash:  base64.StdEncoding.EncodeToString(s.InfoHash),
		Info:      base64.StdEncoding.EncodeToString(s.Info),
		Bitfield:  base64.StdEncoding.EncodeToString(s.Bitfield),
		SeededFor: int64(s.SeededFor),
	}
	return json.Marshal(j)
}

// UnmarshalJSON fills the Spec from a JSON string.
func (s *Spec) UnmarshalJSON(b []byte) error {
	var j jsonSpec
	err := json.Unmarshal(b, &j)
	if err != nil {
		return err
	}
	s.InfoHash, err = base64.StdEncoding.DecodeString(j.InfoHash)
	if err != nil {
		return err
	}
	s.Info, err = base64.StdEncoding.DecodeString(j.Info)
	if err != nil {
		return err
	}
	s.Bitfield, err = base64.StdEncoding.DecodeString(j.Bitfield)
	if err != nil {
		return err
	}
	s.SeededFor = time.Duration(j.SeededFor)
	s.Port = j.Port
	s.Name = j.Name
	s.Trackers = j.Trackers
	s.URLList = j.URLList
	s.FixedPeers = j.FixedPeers
	s.AddedAt = j.AddedAt
	s.BytesDownloaded = j.BytesDownloaded
	s.BytesUploaded = j.BytesUploaded
	s.BytesWasted = j.BytesWasted
	s.Started = j.Started
	s.StopAfterDownload = j.StopAfterDownload
	return nil
}
