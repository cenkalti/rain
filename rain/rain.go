package rain

import (
	"crypto/rand"
	"fmt"
)

type Rain struct {
	peerID []byte
}

func New() *Rain {
	peerID := make([]byte, 20)

	_, err := rand.Read(peerID)
	if err != nil {
		panic(err)
	}

	return &Rain{peerID: peerID}
}

func (r *Rain) Download(filePath string) error {
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
