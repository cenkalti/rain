package rain

import (
	"crypto/rand"
	"net"
	"sync"
	"time"
)

type Rain struct {
	peerID     peerID
	listener   *net.TCPListener
	transfers  map[infoHash]*transfer
	transfersM sync.Mutex
	log        logger
}

// http://www.bittorrent.org/beps/bep_0020.html
var peerIDPrefix = []byte("-RN0001-")

type peerID [20]byte

// New returns a pointer to new Rain BitTorrent client.
func New(port int) (*Rain, error) {
	peerID, err := generatePeerID()
	if err != nil {
		return nil, err
	}
	r := &Rain{
		peerID:    peerID,
		transfers: make(map[infoHash]*transfer),
		log:       newLogger("rain"),
	}
	if err = r.listenPeerPort(port); err != nil {
		return nil, err
	}
	return r, nil
}

func generatePeerID() (peerID, error) {
	var id peerID
	copy(id[:], peerIDPrefix)
	_, err := rand.Read(id[len(peerIDPrefix):])
	return id, err
}

// listenPeerPort starts to listen a TCP port to accept incoming peer connections.
func (r *Rain) listenPeerPort(port int) error {
	var err error
	addr := &net.TCPAddr{Port: port}
	r.listener, err = net.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	r.log.Notice("Listening peers on tcp://" + r.listener.Addr().String())
	go r.accepter()
	return nil
}

func (r *Rain) accepter() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			r.log.Error(err)
			return
		}
		p := newPeerConn(conn)
		go r.servePeerConn(p)
	}
}

func (r *Rain) servePeerConn(p *peerConn) {
	defer p.conn.Close()
	p.log.Debugln("Serving peer", p.conn.RemoteAddr())

	// Give a minute for completing handshake.
	err := p.conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	var t *transfer

	resultC := make(chan interface{}, 2)
	go p.readHandShake(resultC)

	// Send handshake as soon as you see info_hash.
	i := <-resultC
	switch res := i.(type) {
	case infoHash:
		// Do not continue if we don't have a torrent with this infoHash.
		r.transfersM.Lock()
		var ok bool
		if t, ok = r.transfers[res]; !ok {
			p.log.Error("unexpected info_hash")
			r.transfersM.Unlock()
			return
		}
		r.transfersM.Unlock()

		if err = p.sendHandShake(res, r.peerID); err != nil {
			p.log.Error(err)
			return
		}
	case error:
		p.log.Error(res)
		return
	}

	i = <-resultC
	switch res := i.(type) {
	case peerID:
		if res == r.peerID {
			p.log.Debug("Rejected own connection: server")
			return
		}
		// TODO save peer_id
	case error:
		p.log.Error(res)
		return
	}

	p.log.Debugln("servePeerConn: Handshake completed", p.conn.RemoteAddr())
	p.run(t)
}

func (r *Rain) connectToPeerAndServeDownload(addr *net.TCPAddr, t *transfer) {
	r.log.Debugln("Connecting to peer", addr)

	conn, err := net.DialTCP("tcp4", nil, addr)
	if err != nil {
		r.log.Error(err)
		return
	}
	defer conn.Close()

	p := newPeerConn(conn)
	p.log.Infoln("Connected to peer", conn.RemoteAddr())

	// Give a minute for completing handshake.
	err = conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	err = p.sendHandShake(t.torrentFile.InfoHash, r.peerID)
	if err != nil {
		p.log.Error(err)
		return
	}

	ih, id, err := p.readHandShakeBlocking()
	if err != nil {
		p.log.Error(err)
		return
	}
	if *ih != t.torrentFile.InfoHash {
		p.log.Error("unexpected info_hash")
		return
	}
	if *id == r.peerID {
		p.log.Debug("Rejected own connection: client")
		return
	}

	p.log.Debugln("connectToPeer: Handshake completed", conn.RemoteAddr())
	pc := newPeerConn(conn)
	pc.run(t)
}

// Download starts a download and waits for it to finish.
func (r *Rain) Download(torrentPath, where string) error {
	torrent, err := newTorrentFile(torrentPath)
	if err != nil {
		return err
	}
	r.log.Debugf("Parsed torrent file: %#v", torrent)

	r.transfersM.Lock()
	t, err := newTransfer(torrent, where)
	if err != nil {
		r.transfersM.Unlock()
		return err
	}
	r.transfers[torrent.InfoHash] = t
	r.transfersM.Unlock()

	return r.run(t)
}

func (r *Rain) run(t *transfer) error {
	tracker, err := newTracker(t.torrentFile.Announce, r.peerID, uint16(r.listener.Addr().(*net.TCPAddr).Port))
	if err != nil {
		return err
	}

	go r.announcer(tracker, t)

	for _, p := range t.pieces {
		go p.run()
	}

	select {}
}

func (r *Rain) announcer(tracker *tracker, t *transfer) {
	err := tracker.Dial()
	if err != nil {
		t.log.Fatal(err)
	}

	responseC := make(chan *announceResponse)
	go tracker.announce(t, nil, nil, responseC)

	for {
		select {
		case resp := <-responseC:
			tracker.log.Debugf("Announce response: %#v", resp)
			for _, p := range resp.Peers {
				tracker.log.Debug("Peer:", p.TCPAddr())
				go r.connectToPeerAndServeDownload(p.TCPAddr(), t)
			}
		}
	}
}
