package rain

import (
	"fmt"
	"github.com/cenkalti/hub"
)

// Events
const (
	DownloadFinished hub.Kind = iota
)

// Download represents an active download in the program.
type Download struct {
	TorrentFile *TorrentFile
	Downloaded  int64
	Left        int64
	Uploaded    int64
	Events      *hub.Hub
}

func NewDownload(t *TorrentFile) *Download {
	return &Download{
		TorrentFile: t,
		Events:      hub.New(),
	}
}

func (d *Download) Start() {
	tracker, err := NewTracker(d.TorrentFile.Announce)
	if err != nil {
		panic(err)
	}

	_, err = tracker.Connect()
	if err != nil {
		panic(err)
	}

	ann, err := tracker.Announce(d)
	if err != nil {
		panic(err)
	}
	fmt.Printf("--- ann: %#v\n", ann)
}
