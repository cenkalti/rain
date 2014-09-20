package rain

import (
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/cenkalti/mse"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/connection"
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
	sKey := mse.HashSKey(t.torrent.Info.Hash[:])

	t.rain.transfersM.Lock()
	t.rain.transfers[t.torrent.Info.Hash] = t
	t.rain.transfersSKey[sKey] = t
	t.rain.transfersM.Unlock()

	defer func() {
		t.rain.transfersM.Lock()
		delete(t.rain.transfers, t.torrent.Info.Hash)
		delete(t.rain.transfersSKey, sKey)
		t.rain.transfersM.Unlock()
	}()

	announceC := make(chan *tracker.AnnounceResponse)
	go tracker.AnnouncePeriodically(t.tracker, t, nil, tracker.Started, nil, announceC)

	downloader := newDownloader(t)
	go downloader.Run()

	uploader := newUploader(t)
	go uploader.Run()

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
	conn, _, ext, _, err := connection.Dial(addr, !t.rain.config.Encryption.DisableOutgoing, t.rain.config.Encryption.ForceOutgoing, [8]byte{}, t.torrent.Info.Hash, t.rain.peerID)
	if err != nil {
		if err == connection.ErrOwnConnection {
			t.log.Debug(err)
		} else {
			t.log.Error(err)
		}
		return
	}
	defer conn.Close()
	p := newPeer(conn)
	p.log.Info("Connected to peer")
	p.log.Debugf("Peer extensions: %s", ext)
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
