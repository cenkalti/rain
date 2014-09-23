package rain

import (
	"errors"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/tracker"
)

type downloader struct {
	transfer    *transfer
	remaining   []*piece.Piece
	peersC      chan []*net.TCPAddr
	peerC       chan *net.TCPAddr
	haveC       chan *peer.Have
	haveNotifyC chan struct{}
	requestC    chan chan *piece.Piece
	responseC   chan *piece.Piece
	blockC      chan *peer.Block
	cancelC     chan struct{}
	port        uint16
	peers       map[uint32][]*peer.Peer
	peersM      sync.Mutex
	log         logger.Logger
}

func newDownloader(t *transfer) *downloader {
	remaining := make([]*piece.Piece, 0, len(t.pieces))
	for i := uint32(0); i < t.bitField.Len(); i++ {
		if !t.bitField.Test(i) {
			remaining = append(remaining, t.pieces[i])
		}
	}
	return &downloader{
		transfer:    t,
		remaining:   remaining,
		peersC:      make(chan []*net.TCPAddr),
		peerC:       make(chan *net.TCPAddr, tracker.NumWant),
		haveNotifyC: make(chan struct{}, 1),
		requestC:    make(chan chan *piece.Piece),
		responseC:   make(chan *piece.Piece),
		blockC:      make(chan *peer.Block),
		cancelC:     make(chan struct{}),
		port:        t.rain.Port(),
		haveC:       make(chan *peer.Have),
		peers:       make(map[uint32][]*peer.Peer),
		log:         t.log,
	}
}

func (d *downloader) BlockC() chan *peer.Block { return d.blockC }
func (d *downloader) HaveC() chan *peer.Have   { return d.haveC }

func (d *downloader) Run() {
	t := d.transfer
	t.log.Debug("started downloader")

	left := len(d.remaining)

	go d.connecter()
	go d.peerManager()
	go d.pieceRequester()

	// Download pieces in parallel.
	for i := 0; i < maxPeerPerTorrent; i++ {
		go d.pieceDownloader()
	}

	for {
		select {
		case p := <-d.responseC:
			t.bitField.Set(p.Index()) // #####
			left--
			if left == 0 {
				t.log.Notice("Download finished.")
				close(t.Finished)
				return
			}
		case have := <-d.haveC:
			d.peersM.Lock()
			d.peers[have.Piece.Index()] = append(d.peers[have.Piece.Index()], have.Peer)
			d.peersM.Unlock()

			select {
			case d.haveNotifyC <- struct{}{}:
			default:
			}
		case <-d.cancelC:
			return
		}
	}
}

// peerManager receives from d.peersC and keeps most recent tracker.NumWant peer addresses in d.peerC.
func (d *downloader) peerManager() {
	for {
		select {
		case peers := <-d.peersC:
			for _, peer := range peers {
				d.log.Debug("Peer:", peer)
				select {
				case d.peerC <- peer:
				default:
					<-d.peerC
					d.peerC <- peer
				}
			}
		case <-d.cancelC:
			return
		}
	}
}

// connecter connects to peers coming from d. peerC.
func (d *downloader) connecter() {
	limit := make(chan struct{}, maxPeerPerTorrent)
	for {
		select {
		case p := <-d.peerC:
			if p.Port == 0 {
				break
			}
			if p.IP.IsLoopback() && p.Port == int(d.port) {
				break
			}

			limit <- struct{}{}
			go func(addr *net.TCPAddr) {
				defer func() {
					if err := recover(); err != nil {
						buf := make([]byte, 10000)
						d.transfer.log.Critical(err, "\n", string(buf[:runtime.Stack(buf, false)]))
					}
					<-limit
				}()
				d.connect(addr)
			}(p)
		case <-d.cancelC:
			return
		}
	}
}

func (d *downloader) connect(addr *net.TCPAddr) {
	t := d.transfer
	conn, _, _, _, err := connection.Dial(addr, !t.rain.config.Encryption.DisableOutgoing, t.rain.config.Encryption.ForceOutgoing, [8]byte{}, t.torrent.Info.Hash, t.rain.peerID)
	if err != nil {
		if err == connection.ErrOwnConnection {
			t.log.Debug(err)
		} else {
			t.log.Error(err)
		}
		return
	}
	defer conn.Close()

	p := peer.New(conn, peer.Outgoing, t)
	t.log.Infoln("Connected to peer", addr.String())

	if err = p.SendBitField(); err != nil {
		t.log.Error(err)
		return
	}

	d.transfer.peersM.Lock()
	d.transfer.peers[p] = struct{}{}
	d.transfer.peersM.Unlock()
	defer func() {
		d.transfer.peersM.Lock()
		delete(d.transfer.peers, p)
		d.transfer.peersM.Unlock()
	}()

	p.Serve()
}

// pieceRequester selects a piece to be downloaded next and sends it to d.requestC.
func (d *downloader) pieceRequester() {
	const waitDuration = time.Second
	for {
		// sync with downloaders
		req := make(chan *piece.Piece)
		select {
		case d.requestC <- req:
		case <-d.cancelC:
			return
		}

		// select a piece to download
		var i int
		for {
			if len(d.remaining) == 0 {
				return
			}

			var err error
			i, err = d.selectPiece()
			if err == nil {
				break // success, selected a piece
			}
			d.transfer.log.Debug(err)

			// Block until we have next "have" message
			select {
			case <-d.haveNotifyC:
				// Do not try to select piece on first "have" message. Wait for more messages for better selection.
				time.Sleep(waitDuration)
				continue
			case <-d.cancelC:
				return
			}
		}

		piece := d.remaining[i]
		d.transfer.log.Debugln("Selected piece:", piece.Index())

		// delete selected
		d.remaining[i], d.remaining = d.remaining[len(d.remaining)-1], d.remaining[:len(d.remaining)-1]

		// send piece to downloaders
		select {
		case req <- piece:
		case <-d.cancelC:
			return
		}
	}
}

// pieceDownloader receives a piece from d.requestC and downloads it.
func (d *downloader) pieceDownloader() {
	for {
		select {
		case req := <-d.requestC:
			piece, ok := <-req
			if !ok {
				continue
			}

			if err := d.downloadPiece(piece); err != nil {
				d.transfer.log.Error(err)
				// responseC <- nil
				continue
			}
			d.transfer.BitField().Set(piece.Index())

			select {
			case d.responseC <- piece:
			case <-d.cancelC:
				return
			}
		case <-d.cancelC:
			return
		}
	}
}

// selectPiece returns the index of the piece in pieces.
func (d *downloader) selectPiece() (int, error) {
	var pieces []t // pieces with peers
	d.peersM.Lock()
	for i, p := range d.remaining {
		if len(d.peers[p.Index()]) > 0 {
			pieces = append(pieces, t{i, p})
		}
	}
	d.peersM.Unlock()
	if len(pieces) == 0 {
		return -1, errNoPiece
	}
	if len(pieces) == 1 {
		return 0, nil
	}
	// sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))].i, nil
}

type t struct {
	i int
	p *piece.Piece
}

// // Implements sort.Interface based on availability of piece.
// type rarestFirst []t

// func (r rarestFirst) Len() int           { return len(r) }
// func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
// func (r rarestFirst) Less(i, j int) bool { return len(r[i].p.peers) < len(r[j].p.peers) }

var errNoPiece = errors.New("no piece available for download")
var errNoPeer = errors.New("no peer available for this piece")

func (d *downloader) downloadPiece(p *piece.Piece) error {
	d.transfer.log.Debug("downloading")

	peer, err := d.selectPeer(p)
	if err != nil {
		return err
	}
	d.transfer.log.Debugln("selected peer:", peer)

	return peer.DownloadPiece(p)
}

func (d *downloader) selectPeer(p *piece.Piece) (*peer.Peer, error) {
	d.peersM.Lock()
	defer d.peersM.Unlock()
	peers := d.peers[p.Index()]
	if len(peers) == 0 {
		return nil, errNoPeer
	}
	return peers[rand.Intn(len(d.peers))], nil
}
