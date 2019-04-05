package boltdbresumer

import "time"

type Spec struct {
	InfoHash        []byte
	Dest            string
	Port            int
	Name            string
	Trackers        []string
	URLList         []string
	FixedPeers      []string
	Info            []byte
	Bitfield        []byte
	AddedAt         time.Time
	BytesDownloaded int64
	BytesUploaded   int64
	BytesWasted     int64
	SeededFor       time.Duration
}
