package rain

import (
	"crypto/rand"
	"fmt"

	"github.com/cenkalti/hub"
)

// http://www.bittorrent.org/beps/bep_0020.html
var PeerIDPrefix = []byte("-RN0001-")

type Rain struct {
	peerID [20]byte
	// downloads map[[20]byte]*Download
	// trackers  map[string]*Tracker
}

func New() (*Rain, error) {
	r := &Rain{
	// downloads: make(map[[20]byte]*Download),
	// trackers:  make(map[string]*Tracker),
	}
	if err := r.generatePeerID(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rain) generatePeerID() error {
	buf := make([]byte, len(r.peerID)-len(PeerIDPrefix))
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	copy(r.peerID[:], PeerIDPrefix)
	copy(r.peerID[len(PeerIDPrefix):], buf)
	return nil
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(filePath, where string) error {
	torrent, err := LoadTorrentFile(filePath)
	if err != nil {
		return err
	}
	fmt.Printf("--- torrent: %#v\n", torrent)

	download, err := NewDownload(torrent, r.peerID)
	if err != nil {
		return err
	}

	finished := make(chan bool)
	download.Events.Subscribe(DownloadFinished, func(e hub.Event) {
		close(finished)
	})

	download.Run()
	<-finished
	return nil
}
