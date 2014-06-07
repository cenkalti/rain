package rain

import (
	"encoding/binary"
	"fmt"
	"github.com/cenkalti/hub"
	"log"
	"net"
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

	for {
		select {
		case r := <-responseC:
			fmt.Printf("--- announce response: %#v\n", r)
			for _, p := range r.Peers {
				fmt.Printf("--- p: %s\n", p.TCPAddr())
				go connectToPeer(p)
			}
			// case
		}
	}
}

func connectToPeer(p *Peer) {
	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("connected to %s\n", conn.RemoteAddr().String())
	binary.Write(conn, binary.BigEndian, uint8(19))
	// binary
}

func (d *Download) run() {
	// TODO
}
