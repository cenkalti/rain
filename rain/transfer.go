package rain

import (
	"errors"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// transfer represents an active transfer in the program.
type transfer struct {
	rain        *Rain
	tracker     *tracker
	torrentFile *torrentFile
	pieces      []*piece
	bitField    bitField // pieces that we have
	downloaded  chan struct{}
	haveC       chan peerHave
	m           sync.Mutex
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
		bitField:    newBitField(nil, uint32(len(pieces))),
		downloaded:  make(chan struct{}),
		haveC:       make(chan peerHave),
		log:         newLogger("download " + name),
	}, nil
}

func (t *transfer) Downloaded() int64 { return 0 } // TODO
func (t *transfer) Uploaded() int64   { return 0 } // TODO
func (t *transfer) Left() int64       { return t.torrentFile.Info.TotalLength - t.Downloaded() }

func (t *transfer) run() {
	err := t.tracker.Dial()
	if err != nil {
		// TODO retry connecting to tracker
		t.log.Fatal(err)
	}

	peers := make(chan peerAddr, numWant)
	go t.connecter(peers)

	announceC := make(chan *announceResponse)
	go t.tracker.announce(t, nil, nil, announceC)

	var receivedHaveMessage bool
	startDownloader := make(chan struct{})
	go t.downloader(startDownloader)

	for {
		select {
		case resp := <-announceC:
			t.tracker.log.Debugf("Announce response: %#v", resp)
			for _, peer := range resp.Peers {
				t.tracker.log.Debug("Peer:", peer.TCPAddr())

				select {
				case <-peers:
				default:
				}
				peers <- peer
			}
		// case peerConnected TODO
		// case peerDisconnected TODO
		case peerHave := <-t.haveC:
			t.log.Debugf("received have message: %v", peerHave)
			peer := peerHave.peer
			piece := peerHave.piece
			piece.peers = append(piece.peers, peer)
			if !receivedHaveMessage {
				receivedHaveMessage = true
				close(startDownloader)
			}
		}
	}
}

func (t *transfer) downloader(start chan struct{}) {
	// Wait for a while to get enough "have" messages before starting to download pieces.
	t.log.Debug("starting downloader")
	<-start
	time.Sleep(4 * time.Second)
	t.log.Debug("started downloader")

	missing := t.bitField.Len() - t.bitField.Count()
	for missing > 0 {
		if t.bitField.All() {
			close(t.downloaded)
			break
		}

		piece, err := t.selectPiece()
		if err != nil {
			t.log.Debug(err)
			// TODO wait for signal
			time.Sleep(4 * time.Second)
			continue
		}
		piece.log.Debug("selected")

		// TODO download pieces in parallel
		// TODO limit max simultaneous piece request.
		// TODO If the piece is not downloaded in a minute, start downloading next piece.
		time.Sleep(2 * time.Second)
		err = piece.download()
		if err != nil {
			// TODO handle error case
			t.log.Fatal(err)
		}

		missing--
	}

	t.log.Notice("Finished")
}

var errNoPiece = errors.New("no piece")
var errNoPeer = errors.New("no peer")

// TODO refactor and return error
func (t *transfer) selectPiece() (*piece, error) {
	var pieces []*piece
	for _, p := range t.pieces {
		if !p.downloaded && len(p.peers) > 0 {
			pieces = append(pieces, p)
		}
	}
	if len(pieces) == 0 {
		return nil, errNoPiece
	}
	if len(pieces) == 1 {
		return pieces[0], nil
	}
	sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))], nil
}

func (t *transfer) connecter(peers chan peerAddr) {
	limit := make(chan struct{}, 25)
	for p := range peers {
		limit <- struct{}{}
		go func(peer peerAddr) {
			t.connectToPeer(peer.TCPAddr())
			<-limit
		}(p)
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

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
