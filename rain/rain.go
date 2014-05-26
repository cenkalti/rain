package rain

import (
	"crypto/rand"
	"fmt"
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

func (r *Rain) Download(filePath, where string) error {
	// rand.Seed(time.Now().UnixNano())

	mi := new(TorrentFile)
	if err := mi.Load(filePath); err != nil {
		return err
	}
	fmt.Printf("--- mi: %#v\n", mi)

	download := &Download{
		TorrentFile: mi,
	}

	tracker, err := NewTracker(mi.Announce)
	if err != nil {
		return err
	}

	_, err = tracker.Connect()
	if err != nil {
		return err
	}

	ann, err := tracker.Announce(download)
	if err != nil {
		return err
	}
	fmt.Printf("--- ann: %#v\n", ann)

	return nil
}
