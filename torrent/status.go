package torrent

import (
	"strconv"
)

type Status int

const (
	Stopped Status = iota
	Downloading
	Seeding
)

var statusStrings = map[Status]string{
	0: "Stopped",
	1: "Downloading",
	2: "Seeding",
}

func (m Status) String() string {
	s, ok := statusStrings[m]
	if !ok {
		return strconv.FormatInt(int64(m), 10)
	}
	return s
}

func (t *Torrent) status() Status {
	if !t.running {
		return Stopped
	}
	if !t.completed {
		return Downloading
	}
	return Seeding
}
