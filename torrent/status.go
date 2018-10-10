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

func (m Status) MarshalText() ([]byte, error) {
	return []byte(m.String()), nil
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
	if t.completed() {
		return Seeding
	}
	if t.info == nil {
		return DownloadingMetadata
	}
	return Downloading
}

func (t *Torrent) running() bool {
	return t.errC != nil
}

func (t *Torrent) completed() bool {
	select {
	case <-t.completeC:
		return true
	default:
		return false
	}
}
