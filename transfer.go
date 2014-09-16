package rain

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cenkalti/mse"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/torrent"
	"github.com/cenkalti/rain/internal/tracker"
)

// transfer represents an active transfer in the program.
type transfer struct {
	rain     *Rain
	tracker  tracker.Tracker
	torrent  *torrent.Torrent
	pieces   []*piece
	bitField bitfield.BitField // pieces that we have
	Finished chan struct{}
	haveC    chan peerHave
	peers    map[*peer]struct{}
	peersM   sync.RWMutex
	log      logger.Logger
}

func (r *Rain) newTransfer(tor *torrent.Torrent, where string) (*transfer, error) {
	tracker, err := tracker.New(tor.Announce, r)
	if err != nil {
		return nil, err
	}
	files, err := allocate(tor.Info, where)
	if err != nil {
		return nil, err
	}
	pieces := newPieces(tor.Info, files)
	name := tor.Info.Name
	if len(name) > 8 {
		name = name[:8]
	}
	return &transfer{
		rain:     r,
		tracker:  tracker,
		torrent:  tor,
		pieces:   pieces,
		bitField: bitfield.New(nil, uint32(len(pieces))),
		Finished: make(chan struct{}),
		haveC:    make(chan peerHave),
		peers:    make(map[*peer]struct{}),
		log:      logger.New("download " + name),
	}, nil
}

func (t *transfer) InfoHash() protocol.InfoHash { return t.torrent.Info.Hash }
func (t *transfer) Downloaded() int64           { return int64(t.bitField.Count() * t.torrent.Info.PieceLength) }
func (t *transfer) Uploaded() int64             { return 0 } // TODO
func (t *transfer) Left() int64                 { return t.torrent.Info.TotalLength - t.Downloaded() }

func (t *transfer) Run() {
	t.rain.transfersM.Lock()
	t.rain.transfers[t.torrent.Info.Hash] = t
	t.rain.transfersM.Unlock()

	defer func() {
		t.rain.transfersM.Lock()
		delete(t.rain.transfers, t.torrent.Info.Hash)
		t.rain.transfersM.Unlock()
	}()

	announceC := make(chan *tracker.AnnounceResponse)
	go tracker.AnnouncePeriodically(t.tracker, t, nil, tracker.Started, nil, announceC)

	downloader := newDownloader(t)
	go downloader.Run()

	// uploader := newUploader(t)
	// go uploader.Run()

	for {
		select {
		case announceResponse := <-announceC:
			t.log.Infof("Announce: %d seeder, %d leecher", announceResponse.Seeders, announceResponse.Leechers)
			downloader.peersC <- announceResponse.Peers
		case peerHave := <-t.haveC:
			piece := peerHave.piece
			piece.peersM.Lock()
			piece.peers = append(piece.peers, peerHave.peer)
			piece.peersM.Unlock()

			select {
			case downloader.haveNotifyC <- struct{}{}:
			default:
			}
		}
	}
}

func (t *transfer) connectToPeer(addr *net.TCPAddr) {
	log := logger.New("peer -> " + addr.String())

	var conn net.Conn
	var err error
	log.Debug("Connecting to peer")
	conn, err = net.DialTCP("tcp4", nil, addr)
	if err != nil {
		log.Error(err)
		return
	}
	defer conn.Close()
	log.Debug("Connected")

	// Outgoing handshake bytes
	b := make([]byte, 68)
	b[0] = protocol.PstrLen
	copy(b[1:], protocol.Pstr)
	copy(b[20:], []byte{}) // no extensions for now
	copy(b[28:], t.torrent.Info.Hash[:])
	peerID := t.rain.PeerID()
	copy(b[48:], peerID[:])

	if !t.rain.config.Encryption.DisableOutgoing {
		// Try encryption handshake
		encConn := mse.WrapConn(conn)
		sKey := make([]byte, 20)
		copy(sKey, t.torrent.Info.Hash[:])
		selected, err := encConn.HandshakeOutgoing(sKey, mse.RC4, b)
		if err != nil {
			log.Debugln("Encrytpion handshake has failed: ", err)
			if !t.rain.config.Encryption.ForceOutgoing {
				// Connect again and try w/o encryption
				log.Debug("Connecting again for unencrypted handshake...")
				conn, err = net.DialTCP("tcp4", nil, addr)
				if err != nil {
					log.Error(err)
					return
				}
				defer conn.Close()
				log.Debug("Connected")

				// Send BT handshake
				conn.SetDeadline(time.Now().Add(30 * time.Second))
				if _, err = conn.Write(b); err != nil {
					log.Error(err)
					return
				}
			} else {
				log.Debug("Will not try again because ougoing encryption is forced.")
			}
		} else {
			log.Debugf("Encryption handshake is successfull. Selected cipher: %d", selected)
			conn = encConn
		}
	} else {
		// Send BT handshake
		conn.SetDeadline(time.Now().Add(30 * time.Second))
		if _, err = conn.Write(b); err != nil {
			log.Error(err)
			return
		}
	}

	_, ih, err := readHandShake1(conn)
	if err != nil {
		log.Error(err)
		return
	}
	if *ih != t.torrent.Info.Hash {
		log.Error("unexpected info_hash")
		return
	}

	id, err := readHandShake2(conn)
	if err != nil {
		log.Error(err)
		return
	}
	if *id == t.rain.peerID {
		log.Debug("Rejected own connection: client")
		return
	}

	log.Info("Connected to peer")
	p := newPeer(conn)
	p.Serve(t)
}

func allocate(info *torrent.Info, where string) ([]*os.File, error) {
	if !info.MultiFile {
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
