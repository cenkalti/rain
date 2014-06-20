package rain

import (
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"time"
)

// transfer represents an active transfer in the program.
type transfer struct {
	rain        *Rain
	tracker     *tracker
	torrentFile *torrentFile
	pieces      []*piece
	bitField    bitField // pieces that we have
	requestC    chan *piece
	log         logger
}

func (r *Rain) newTransfer(tor *torrentFile, where string) (*transfer, error) {
	tracker, err := newTracker(tor.Announce, r.peerID, r.port())
	if err != nil {
		return nil, err
	}
	files, err := allocate(&tor.Info, where)
	if err != nil {
		return nil, err
	}
	pieces := newPieces(&tor.Info, files)
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	return &transfer{
		rain:        r,
		tracker:     tracker,
		torrentFile: tor,
		pieces:      pieces,
		bitField:    newBitField(nil, int32(len(pieces))),
		requestC:    make(chan *piece),
		log:         newLogger("download " + name),
	}, nil
}

func (t *transfer) Downloaded() int64 { return 0 } // TODO
func (t *transfer) Uploaded() int64   { return 0 } // TODO
func (t *transfer) Left() int64       { return t.torrentFile.Info.TotalLength - t.Downloaded() }

// pieceManager decides which piece should we download.
func (t *transfer) pieceManager() {
	// Request pieces in random order.
	for _, v := range rand.Perm(len(t.pieces)) {
		piece := t.pieces[v]

		// Skip downloaded pieces.
		select {
		case <-piece.downloaded:
			continue
		default:
		}

		// Send a request to peerManager.
		// TODO limit max simultaneous piece request.
		t.requestC <- piece

		// If the piece is not downloaded in a minute, start downloading next piece.
		select {
		case <-piece.downloaded:
			piece.log.Debug("downloaded successfully")
		case <-time.After(time.Minute):
			continue
		}
	}
}

// peerManager decides which peer should we download from.
func (t *transfer) peerManager() {
	for piece := range t.requestC {
		// Pick any connected peer randomly.
		peer := piece.peers[rand.Intn(len(piece.peers))]
		go peer.downloadPiece(piece) // TODO handle returned error
	}
}

func (t *transfer) run() {
	err := t.tracker.Dial()
	if err != nil {
		// TODO retry connecting to tracker
		t.log.Fatal(err)
	}

	announceC := make(chan *announceResponse)
	go t.tracker.announce(t, nil, nil, announceC)
	go t.pieceManager()
	go t.peerManager()
	for _, p := range t.pieces {
		go p.run()
	}

	for {
		select {
		case resp := <-announceC:
			t.tracker.log.Debugf("Announce response: %#v", resp)
			for _, p := range resp.Peers {
				t.tracker.log.Debug("Peer:", p.TCPAddr())
				go t.connectToPeer(p.TCPAddr())
			}
		}
	}
}

func (t *transfer) connectToPeer(addr *net.TCPAddr) {
	t.log.Debugln("Connecting to peer", addr)

	conn, err := net.DialTCP("tcp4", nil, addr)
	if err != nil {
		t.log.Error(err)
		return
	}
	defer conn.Close()

	p := newPeerConn(conn)
	p.log.Infoln("Connected to peer")

	// Give a minute for completing handshake.
	err = conn.SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		return
	}

	err = p.sendHandShake(t.torrentFile.Info.Hash, t.rain.peerID)
	if err != nil {
		p.log.Error(err)
		return
	}

	ih, id, err := p.readHandShakeBlocking()
	if err != nil {
		p.log.Error(err)
		return
	}
	if *ih != t.torrentFile.Info.Hash {
		p.log.Error("unexpected info_hash")
		return
	}
	if *id == t.rain.peerID {
		p.log.Debug("Rejected own connection: client")
		return
	}

	p.log.Debugln("connectToPeer: Handshake completed", conn.RemoteAddr())
	pc := newPeerConn(conn)
	pc.run(t)
}

func allocate(info *infoDict, where string) ([]*os.File, error) {
	if !info.MultiFile() {
		f, err := createTruncateSync(filepath.Join(where, info.Name), info.Length)
		if err != nil {
			return nil, err
		}
		return []*os.File{f}, nil
	}

	// Multiple files
	files := make([]*os.File, len(info.Files))
	for i, f := range info.Files {
		parts := append([]string{where, info.Name}, f.Path...)
		path := filepath.Join(parts...)
		err := os.MkdirAll(filepath.Dir(path), os.ModeDir|0755)
		if err != nil {
			return nil, err
		}
		files[i], err = createTruncateSync(path, f.Length)
		if err != nil {
			return nil, err
		}
	}
	return files, nil
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

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}
