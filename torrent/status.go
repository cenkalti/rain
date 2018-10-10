package torrent

import (
	"strconv"
)

type Status int

const (
	Stopped Status = iota
	DownloadingMetadata
	Allocating
	Verifying
	Downloading
	Seeding
)

var statusStrings = map[Status]string{
	0: "Stopped",
	1: "Downloading Metadata",
	2: "Allocating",
	3: "Verifying",
	4: "Downloading",
	5: "Seeding",
}

func (m Status) String() string {
	s, ok := statusStrings[m]
	if !ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return s
}

func (t *Torrent) status() Status {
	if !t.running() {
		return Stopped
	}
	if t.allocator != nil {
		return Allocating
	}
	if t.verifier != nil {
		return Verifying
	}
	if t.completed {
		return Seeding
	}
	if t.info == nil {
		return DownloadingMetadata
	}
	return Downloading
}
