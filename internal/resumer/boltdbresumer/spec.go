package boltdbresumer

import "time"

type Spec struct {
	InfoHash        []byte
	Dest            string
	Port            int
	Name            string
	Trackers        []string
	Info            []byte
	Bitfield        []byte
	CreatedAt       time.Time
	BytesDownloaded int64
	BytesUploaded   int64
	BytesWasted     int64
}
