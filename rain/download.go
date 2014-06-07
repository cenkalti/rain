package rain

import (
	"fmt"
	"github.com/cenkalti/hub"
)

// States
const (
	DownloadStopped = iota
	DownloadRunning
	DownloadSeeding
)

// Events
const (
	DownloadFinished = iota
)

// Download represents an active download in the program.
type Download struct {
	TorrentFile *TorrentFile
	Events      hub.Hub
	tracker     *Tracker
	// Stats
	Downloaded int64
	Uploaded   int64
	// Left       int64
}

func (d *Download) Left() int64 {
	return d.TorrentFile.TotalLength - d.Downloaded
}

func NewDownload(t *TorrentFile, peerID [20]byte) (*Download, error) {
	tracker, err := NewTracker(t.Announce, peerID)
	if err != nil {
		return nil, err
	}

	return &Download{
		TorrentFile: t,
		tracker:     tracker,
	}, nil
}

func (d *Download) Run() {
	err := d.tracker.Dial()
	if err != nil {
		panic(err) // TODO
	}

	responseC := make(chan *AnnounceResponse)
	go d.tracker.announce(d, nil, nil, responseC)
	for r := range responseC {
		fmt.Printf("--- announce response: %#v\n", r)
		for _, p := range r.Peers {
			fmt.Printf("--- p: %s\n", p.Addr())
		}
	}
}

func (d *Download) run() {
	// TODO
}
