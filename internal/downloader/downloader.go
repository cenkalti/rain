package downloader

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/internal/tracker"
	"github.com/cenkalti/rain/peer"
	"github.com/cenkalti/rain/piece"
)

const maxPeerPerTorrent = 200

// TODO remove this
const blockSize = 16 * 1024

type Downloader struct {
	transfer         Transfer
	port             uint16
	peerID           protocol.PeerID
	enableEncryption bool
	forceEncryption  bool
	remaining        []uint32
	peersC           chan []*net.TCPAddr
	peerC            chan *net.TCPAddr
	haveC            chan *peer.HaveMessage
	haveNotifyC      chan struct{}
	requestC         chan chan uint32
	responseC        chan uint32
	pieceC           chan *peer.PieceMessage
	cancelC          chan struct{}
	peers            map[uint32][]*peer.Peer // indexed by piece
	peersM           sync.Mutex
	finished         chan struct{}
	log              logger.Logger
}

type Transfer interface {
	InfoHash() protocol.InfoHash
	BitField() bitfield.BitField
	Piece(i uint32) *piece.Piece
	PieceLength(i uint32) uint32
	Downloader() peer.Downloader
	Uploader() peer.Uploader
	Finished() chan struct{}
}

func New(t Transfer, port uint16, peerID protocol.PeerID, enableEncryption, forceEncryption bool) *Downloader {
	numPieces := t.BitField().Len()
	remaining := make([]uint32, 0, numPieces)
	for i := uint32(0); i < numPieces; i++ {
		if !t.BitField().Test(i) {
			remaining = append(remaining, i)
		}
	}
	return &Downloader{
		transfer:         t,
		port:             port,
		peerID:           peerID,
		enableEncryption: enableEncryption,
		forceEncryption:  forceEncryption,
		remaining:        remaining,
		peersC:           make(chan []*net.TCPAddr),
		peerC:            make(chan *net.TCPAddr, tracker.NumWant),
		haveNotifyC:      make(chan struct{}, 1),
		requestC:         make(chan chan uint32),
		responseC:        make(chan uint32),
		pieceC:           make(chan *peer.PieceMessage),
		cancelC:          make(chan struct{}),
		haveC:            make(chan *peer.HaveMessage),
		peers:            make(map[uint32][]*peer.Peer),
		finished:         make(chan struct{}),
		log:              logger.New("downloader "),
	}
}

func (d *Downloader) PeersC() chan []*net.TCPAddr     { return d.peersC }
func (d *Downloader) PieceC() chan *peer.PieceMessage { return d.pieceC }
func (d *Downloader) HaveC() chan *peer.HaveMessage   { return d.haveC }
func (d *Downloader) Finished() chan struct{}         { return d.finished }

func (d *Downloader) Run() {
	t := d.transfer
	d.log.Debug("started downloader")

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
		case i := <-d.responseC:
			t.BitField().Set(i) // #####
			left--
			if left == 0 {
				d.log.Notice("Download finished.")
				close(d.transfer.Finished())
				return
			}
		case have := <-d.haveC:
			d.peersM.Lock()
			d.peers[have.Index] = append(d.peers[have.Index], have.Peer)
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
func (d *Downloader) peerManager() {
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
func (d *Downloader) connecter() {
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
						d.log.Critical(err, "\n", string(buf[:runtime.Stack(buf, false)]))
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

func (d *Downloader) connect(addr *net.TCPAddr) {
	log := logger.New("peer -> " + addr.String())

	conn, cipher, extensions, _, err := connection.Dial(addr, d.enableEncryption, d.forceEncryption, [8]byte{}, d.transfer.InfoHash(), d.peerID)
	if err != nil {
		if err == connection.ErrOwnConnection {
			log.Debug(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connected to peer. (cipher=%s, extensions=%x)", cipher, extensions)
	defer conn.Close()

	p := peer.New(conn, d.transfer, log)

	if err = p.SendBitField(); err != nil {
		log.Error(err)
		return
	}

	// d.transfer.peersM.Lock()
	// d.transfer.peers[p] = struct{}{}
	// d.transfer.peersM.Unlock()
	// defer func() {
	// 	d.transfer.peersM.Lock()
	// 	delete(d.transfer.peers, p)
	// 	d.transfer.peersM.Unlock()
	// }()

	p.Run()
}

// pieceRequester selects a piece to be downloaded next and sends it to d.requestC.
func (d *Downloader) pieceRequester() {
	const waitDuration = time.Second
	for {
		// sync with downloaders
		req := make(chan uint32)
		select {
		case d.requestC <- req:
		case <-d.cancelC:
			return
		}

		// select a piece to download
		var i uint32
		for {
			if len(d.remaining) == 0 {
				return
			}

			var err error
			i, err = d.selectPiece()
			if err == nil {
				break // success, selected a piece
			}
			d.log.Debug(err)

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

		d.log.Debugln("Selected piece:", i)

		// delete selected
		d.remaining[i], d.remaining = d.remaining[len(d.remaining)-1], d.remaining[:len(d.remaining)-1]

		// send piece to downloaders
		select {
		case req <- i:
		case <-d.cancelC:
			return
		}
	}
}

// pieceDownloader receives a piece from d.requestC and downloads it.
func (d *Downloader) pieceDownloader() {
	for {
		select {
		case req := <-d.requestC:
			i, ok := <-req
			if !ok {
				continue
			}

			if err := d.downloadPiece(i); err != nil {
				d.log.Error(err)
				// responseC <- nil
				continue
			}
			d.transfer.BitField().Set(i)

			select {
			case d.responseC <- i:
			case <-d.cancelC:
				return
			}
		case <-d.cancelC:
			return
		}
	}
}

// selectPiece returns the index of the piece in pieces.
func (d *Downloader) selectPiece() (uint32, error) {
	var pieces []t // pieces with peers
	d.peersM.Lock()
	for i, p := range d.remaining {
		if len(d.peers[p]) > 0 {
			pieces = append(pieces, t{uint32(i), p})
		}
	}
	d.peersM.Unlock()
	if len(pieces) == 0 {
		return 0, errNoPiece
	}
	if len(pieces) == 1 {
		return 0, nil
	}
	// sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))].i, nil
}

type t struct {
	i uint32
	p uint32
}

// // Implements sort.Interface based on availability of piece.
// type rarestFirst []t

// func (r rarestFirst) Len() int           { return len(r) }
// func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
// func (r rarestFirst) Less(i, j int) bool { return len(r[i].p.peers) < len(r[j].p.peers) }

var errNoPiece = errors.New("no piece available for download")
var errNoPeer = errors.New("no peer available for this piece")

func (d *Downloader) downloadPiece(i uint32) error {
	d.log.Debugln("Downloading piece:", i)

	peer, err := d.selectPeer(i)
	if err != nil {
		return err
	}
	d.log.Debugln("selected peer:", peer)

	unchokeC, err := peer.BeInterested()
	if err != nil {
		return err
	}

	select {
	case <-unchokeC:
	case <-peer.Disconnected:
		return errors.New("peer disconnected")
	}

	p := d.transfer.Piece(i)

	// Request blocks of the piece.
	for _, b := range p.Blocks() {
		if err := peer.Request(i, b.Begin, b.Length); err != nil {
			return err
		}
	}

	// Read blocks from peer.
	pieceData := make([]byte, p.Length())
	for _ = range p.Blocks() {
		select {
		case peerBlock := <-d.pieceC:
			data := <-peerBlock.Data
			if data == nil {
				return errors.New("peer did not send block completely")
			}
			d.log.Debugln("Will receive block of length", len(data))
			copy(pieceData[peerBlock.Begin:], data)
		case <-time.After(time.Minute):
			return fmt.Errorf("peer did not send piece #%d completely", p.Index())
		}
	}

	// Verify piece hash.
	hash := sha1.Sum(pieceData)
	if !bytes.Equal(hash[:], p.Hash()) {
		return errors.New("received corrupt piece")
	}

	_, err = p.Write(pieceData)
	return err
}

func (d *Downloader) selectPeer(i uint32) (*peer.Peer, error) {
	d.peersM.Lock()
	defer d.peersM.Unlock()
	peers := d.peers[i]
	if len(peers) == 0 {
		return nil, errNoPeer
	}
	return peers[rand.Intn(len(d.peers))], nil
}
