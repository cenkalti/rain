package rain

import (
	"container/list"
	"os"
	"path/filepath"

	"github.com/cenkalti/log"
)

// transfer represents an active transfer in the program.
type transfer struct {
	torrentFile *TorrentFile
	where       string
	peerID      peerID

	// Stats
	Downloaded int64
	Uploaded   int64
	// Left       int64

	pieces []*piece

	// piece index -> peers that have the piece
	have        map[int32]*list.List
	haveMessage chan *peerConn
}

func newTransfer(tor *TorrentFile, where string, id peerID) *transfer {
	t := &transfer{
		torrentFile: tor,
		where:       where,
		peerID:      id,
		pieces:      make([]*piece, tor.NumPieces),
	}
	for i := 0; i < tor.NumPieces; i++ {
		t.pieces[i] = newPiece(i)
	}
	return t
}

func (t *transfer) Left() int64 {
	// TODO return correct "left bytes"
	return t.torrentFile.TotalLength - t.Downloaded
}

func (t *transfer) allocate(where string) error {
	var err error
	info := &t.torrentFile.Info

	// Single file
	if info.Length != 0 {
		info.file, err = createTruncateSync(filepath.Join(where, info.Name), info.Length)
		return err
	}

	// Multiple files
	for _, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err = os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return err
		}
		f.file, err = createTruncateSync(path, f.Length)
		if err != nil {
			return err
		}
	}
	return nil
}

func createTruncateSync(path string, length int64) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	err = f.Truncate(length)
	if err != nil {
		return nil, err
	}

	err = f.Sync()
	if err != nil {
		return nil, err
	}

	return f, nil
}

func (t *transfer) run(port uint16) error {
	err := t.allocate(t.where)
	if err != nil {
		return err
	}

	tracker, err := NewTracker(t.torrentFile.Announce, t.peerID, port)
	if err != nil {
		return err
	}

	go t.announcer(tracker)
	go t.haveLoop()

	select {}
	return nil
}

func (t *transfer) announcer(tracker *Tracker) {
	err := tracker.Dial()
	if err != nil {
		log.Fatal(err)
	}

	responseC := make(chan *AnnounceResponse)
	go tracker.announce(t, nil, nil, responseC)

	for {
		select {
		case resp := <-responseC:
			log.Debugf("Announce response: %#v", resp)
			for _, p := range resp.Peers {
				log.Debug("Peer:", p.TCPAddr())
				go connectToPeerAndServeDownload(p, t)
			}
		}
	}
}

func (t *transfer) haveLoop() {
	// for p := range t.haveMessage {

	// }
}
