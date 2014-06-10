package rain

import (
	"crypto/rand"
	"net"
	"sync"

	"github.com/cenkalti/log"
)

type Rain struct {
	peerID     *peerID
	listener   *net.TCPListener
	downloads  map[infoHash]*download
	downloadsM sync.Mutex
}

// New returns a pointer to new Rain BitTorrent client.
// Call ListenPeerPort method before starting Download to accept incoming connections.
func New() (*Rain, error) {
	r := &Rain{
		downloads: make(map[infoHash]*download),
	}
	return r, r.generatePeerID()
}

func (r *Rain) generatePeerID() error {
	r.peerID = new(peerID)
	copy(r.peerID[:], peerIDPrefix)
	_, err := rand.Read(r.peerID[len(peerIDPrefix):])
	return err
}

// ListenPeerPort starts to listen a TCP port to accept incoming peer connections.
func (r *Rain) ListenPeerPort(port int) error {
	var err error
	addr := &net.TCPAddr{Port: port}
	r.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	log.Notice("Listening peers on tcp://" + r.listener.Addr().String())
	go r.acceptor()
	return nil
}

func (r *Rain) acceptor() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			log.Error(err)
			return
		}
		go r.servePeerConn(conn)
	}
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(filePath, where string) error {
	torrent, err := NewTorrentFile(filePath)
	if err != nil {
		return err
	}
	log.Debugf("Parsed torrent file: %#v", torrent)

	download := NewDownload(torrent)
	r.downloadsM.Lock()
	r.downloads[download.TorrentFile.InfoHash] = download
	r.downloadsM.Unlock()

	err = download.allocate(where)
	if err != nil {
		return err
	}

	tracker, err := NewTracker(torrent.Announce, r.peerID, uint16(r.listener.Addr().(*net.TCPAddr).Port))
	if err != nil {
		return err
	}

	go r.announcer(tracker, download)

	select {}
	return nil
}

func (r *Rain) announcer(t *Tracker, d *download) {
	err := t.Dial()
	if err != nil {
		log.Fatal(err)
	}

	responseC := make(chan *AnnounceResponse)
	go t.announce(d, nil, nil, responseC)

	for {
		select {
		case resp := <-responseC:
			log.Debugf("Announce response: %#v", resp)
			for _, p := range resp.Peers {
				log.Debug("Peer:", p.TCPAddr())

				go r.connectToPeer(p, d)
			}
			// case
		}
	}
}
