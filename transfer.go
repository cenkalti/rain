package rain

import (
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
)

// transfer represents an active transfer in the program.
type transfer struct {
	rain       *Rain
	tracker    tracker.Tracker
	torrent    *torrent.Torrent
	pieces     []*piece
	bitField   bitfield.BitField // pieces that we have
	downloaded chan struct{}
	haveC      chan peerHave
	log        logger.Logger
}

func (r *Rain) newTransfer(tor *torrent.Torrent, where string) (*transfer, error) {
	tracker, err := tracker.New(tor.Announce, r.peerID, r.port())
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
		rain:       r,
		tracker:    tracker,
		torrent:    tor,
		pieces:     pieces,
		bitField:   bitfield.New(nil, uint32(len(pieces))),
		downloaded: make(chan struct{}),
		haveC:      make(chan peerHave),
		log:        logger.New("download " + name),
	}, nil
}

func (t *transfer) InfoHash() protocol.InfoHash { return t.torrent.Info.Hash }
func (t *transfer) Downloaded() int64           { return int64(t.bitField.Count() * t.torrent.Info.PieceLength) }
func (t *transfer) Uploaded() int64             { return 0 } // TODO
func (t *transfer) Left() int64                 { return t.torrent.Info.TotalLength - t.Downloaded() }

func (t *transfer) run() {
	peers := make(chan tracker.Peer, tracker.NumWant)
	go t.connecter(peers)

	announceC := make(chan []tracker.Peer)
	go t.tracker.Announce(t, nil, nil, announceC)

	var receivedHaveMessage bool
	startDownloader := make(chan struct{})
	go t.downloader(startDownloader)

	for {
		select {
		case peerAddrs := <-announceC:
			for _, pa := range peerAddrs {
				t.log.Debug("Peer:", pa.TCPAddr())
				select {
				case peers <- pa:
				default:
					<-peers
					peers <- pa
				}
			}
		// case peerConnected TODO
		// case peerDisconnected TODO
		case peerHave := <-t.haveC:
			piece := peerHave.piece
			piece.peersM.Lock()
			piece.peers = append(piece.peers, peerHave.peer)
			piece.peersM.Unlock()
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
	time.Sleep(2 * time.Second)
	t.log.Debug("started downloader")

	requestC := make(chan chan *piece)
	responseC := make(chan *piece)

	missing := t.bitField.Len() - t.bitField.Count()

	go t.pieceRequester(requestC)

	// Download pieces in parallel.
	for i := 0; i < maxSimultaneoutPieceDownloadStart; i++ {
		go pieceDownloader(requestC, responseC)
	}

	for p := range responseC {
		t.bitField.Set(p.index)

		missing--
		if missing == 0 {
			break
		}
	}

	t.log.Notice("Finished")
	close(t.downloaded)
}

func (t *transfer) pieceRequester(requestC chan chan *piece) {
	requested := bitfield.New(nil, t.bitField.Len())
	missing := t.bitField.Len() - t.bitField.Count()
	for missing > 0 {
		req := make(chan *piece)
		requestC <- req

		piece, err := t.selectPiece(&requested)
		if err != nil {
			t.log.Debug(err)
			// TODO do not sleep, block until we have next "have" message
			time.Sleep(4 * time.Second)
			continue
		}

		piece.log.Debug("selected")
		requested.Set(piece.index)
		req <- piece
		missing--
	}
	close(requestC)
}

func pieceDownloader(requestC chan chan *piece, responseC chan *piece) {
	for req := range requestC {
		piece, ok := <-req
		if !ok {
			continue
		}

		err := piece.download()
		if err != nil {
			piece.log.Error(err)
			// responseC <- nil
			continue
		}

		responseC <- piece
	}
}

func (t *transfer) selectPiece(requested *bitfield.BitField) (*piece, error) {
	var pieces []*piece
	for i, p := range t.pieces {
		p.peersM.Lock()
		if !requested.Test(uint32(i)) && !p.ok && len(p.peers) > 0 {
			pieces = append(pieces, p)
		}
		p.peersM.Unlock()
	}
	if len(pieces) == 0 {
		return nil, errPieceNotAvailable
	}
	if len(pieces) == 1 {
		return pieces[0], nil
	}
	sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))], nil
}

func (t *transfer) connecter(peers chan tracker.Peer) {
	limit := make(chan struct{}, maxPeerPerTorrent)
	for p := range peers {
		limit <- struct{}{}
		go func(peer tracker.Peer) {
			defer func() {
				if err := recover(); err != nil {
					t.log.Critical(err)
				}
				<-limit
			}()
			t.connectToPeer(peer.TCPAddr())
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

	err = p.sendHandShake(t.torrent.Info.Hash, t.rain.peerID)
	if err != nil {
		p.log.Error(err)
		return
	}

	ih, err := p.readHandShake1()
	if err != nil {
		p.log.Error(err)
		return
	}
	if *ih != t.torrent.Info.Hash {
		p.log.Error("unexpected info_hash")
		return
	}

	id, err := p.readHandShake2()
	if err != nil {
		p.log.Error(err)
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

func allocate(info *torrent.Info, where string) ([]*os.File, error) {
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

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
