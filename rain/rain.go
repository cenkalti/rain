package rain

import (
	"crypto/rand"
	"fmt"

	"github.com/cenkalti/hub"
)

type Rain struct {
	peerID [20]byte
}

func New() (*Rain, error) {
	r := &Rain{}
	if err := r.generatePeerID(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rain) generatePeerID() error {
	buf := make([]byte, 20)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	copy(r.peerID[:], buf)
	return nil
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(filePath, where string) error {
	torrent, err := LoadTorrentFile(filePath)
	if err != nil {
		return err
	}
	fmt.Printf("--- torrent: %#v\n", torrent)

	download := NewDownload(torrent)

	finished := make(chan bool)
	download.Events.Subscribe(DownloadFinished, func(e hub.Event) {
		close(finished)
	})

	download.Start()
	<-finished
	return nil
}
