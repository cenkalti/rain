package rain

import (
	"crypto/rand"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/log"
)

type Rain struct {
	peerID     peerID
	listener   *net.TCPListener
	transfers  map[infoHash]*transfer
	transfersM sync.Mutex
}

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN0001-")

type peerID [20]byte

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

func (r *Rain) servePeerConn(conn net.Conn) {
	defer conn.Close()
	log.Debugln("Serving peer", conn.RemoteAddr())

	// Give a minute for completing handshake.
	err := conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	var d *transfer

	resultC := make(chan interface{}, 2)
	go readHandShake(conn, resultC)

	// Send handshake as soon as you see info_hash.
	i := <-resultC
	switch res := i.(type) {
	case infoHash:
		// Do not continue if we don't have a torrent with this infoHash.
		r.transfersM.Lock()
		var ok bool
		if d, ok = r.transfers[res]; !ok {
			log.Error("unexpected info_hash")
			r.transfersM.Unlock()
			return
		}
		r.transfersM.Unlock()

		if err = sendHandShake(conn, res, r.peerID); err != nil {
			log.Error(err)
			return
		}
	case error:
		log.Error(res)
		return
	}

	i = <-resultC
	switch res := i.(type) {
	case peerID:
		if res == r.peerID {
			log.Debug("Rejected own connection: server")
			return
		}
		// TODO save peer_id
	case error:
		log.Error(res)
		return
	}

	log.Debugln("servePeerConn: Handshake completed", conn.RemoteAddr())
	p := newPeerConn(conn, d)
	p.readLoop()
}

func (r *Rain) connectToPeerAndServeDownload(p *Peer, d *transfer) {
	log.Debugln("Connecting to peer", p.TCPAddr())

	conn, err := net.DialTCP("tcp4", nil, p.TCPAddr())
	if err != nil {
		log.Error(err)
		return
	}
	defer conn.Close()

	log.Infoln("Connected to peer", conn.RemoteAddr())

	// Give a minute for completing handshake.
	err = conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	err = sendHandShake(conn, d.torrentFile.InfoHash, r.peerID)
	if err != nil {
		log.Error(err)
		return
	}

	ih, id, err := readHandShakeBlocking(conn)
	if err != nil {
		log.Error(err)
		return
	}
	if *ih != d.torrentFile.InfoHash {
		log.Error("unexpected info_hash")
		return
	}
	if *id == r.peerID {
		log.Debug("Rejected own connection: client")
		return
	}

	log.Debugln("connectToPeer: Handshake completed", conn.RemoteAddr())
	pc := newPeerConn(conn, d)
	pc.readLoop()
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

	for _, p := range t.pieces {
		go p.run()
	}

	select {}
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
