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

	t := r.newTransfer(torrent, where)
	r.transfersM.Lock()
	r.transfers[t.torrentFile.InfoHash] = t
	r.transfersM.Unlock()

	// TODO do not pass port after implementing tracker manager
	return t.run(uint16(r.listener.Addr().(*net.TCPAddr).Port))
}
