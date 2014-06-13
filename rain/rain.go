package rain

import (
	"crypto/rand"
	"net"
	"sync"

	"github.com/cenkalti/log"
)

type Rain struct {
	peerID     peerID
	listener   *net.TCPListener
	transfers  map[infoHash]*transfer
	transfersM sync.Mutex
}

// New returns a pointer to new Rain BitTorrent client.
// Call ListenPeerPort method before starting Download to accept incoming connections.
func New() (*Rain, error) {
	peerID, err := generatePeerID()
	if err != nil {
		return nil, err
	}
	return &Rain{
		peerID:    peerID,
		transfers: make(map[infoHash]*transfer),
	}, nil
}

func generatePeerID() (peerID, error) {
	var id peerID
	copy(id[:], peerIDPrefix)
	_, err := rand.Read(id[len(peerIDPrefix):])
	return id, err
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
	go r.accepter()
	return nil
}

func (r *Rain) accepter() {
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
func (r *Rain) Download(torrentPath, where string) error {
	torrent, err := NewTorrentFile(torrentPath)
	if err != nil {
		return err
	}
	log.Debugf("Parsed torrent file: %#v", torrent)

	t := newTransfer(torrent, where)
	r.transfersM.Lock()
	r.transfers[t.torrentFile.InfoHash] = t
	r.transfersM.Unlock()

	return r.run(t)
}

func (r *Rain) run(t *transfer) error {
	err := t.allocate(t.where)
	if err != nil {
		return err
	}

	tracker, err := NewTracker(t.torrentFile.Announce, r.peerID, uint16(r.listener.Addr().(*net.TCPAddr).Port))
	if err != nil {
		return err
	}

	go r.announcer(tracker, t)
	go t.haveLoop()

	var wg sync.WaitGroup
	wg.Add(len(t.pieces))
	for _, p := range t.pieces {
		go p.download(&wg)
	}

	wg.Wait()
	log.Notice("Download finished.")
	select {}
	return nil
}

func (r *Rain) announcer(tracker *Tracker, t *transfer) {
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
				go r.connectToPeerAndServeDownload(p, t)
			}
		}
	}
}
