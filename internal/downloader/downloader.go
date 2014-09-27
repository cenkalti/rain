package downloader

import (
	"math/rand"
	"net"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/protocol"
	"github.com/cenkalti/rain/peer"
	"github.com/cenkalti/rain/piece"
)

const maxPeerPerTorrent = 200

type Downloader struct {
	transfer         Transfer
	port             uint16
	peerID           protocol.PeerID
	enableEncryption bool
	forceEncryption  bool
	// indexes of the pieces that needs to be downloaded
	remaining map[uint32]struct{}
	requested map[uint32]time.Time
	// tracker sends available peers to this channel
	peersC chan []*net.TCPAddr
	// pieceManager maintains most recent peers which can be connected to in this channel
	// connecter receives from this channel and connects to new peers
	peerC chan *net.TCPAddr
	// peers send have messages to this channel
	haveC chan *peer.HaveMessage
	// downloaded piece indexes are sent to this channel by peer downloaders
	completeC chan uint32
	pieceC    chan *peer.PieceMessage
	// all goroutines are stopped when this channel is closed
	cancelC chan struct{}
	// holds pieces are available in which peers, indexed by piece
	peersByPiece []map[*peer.Peer]struct{}
	// holds peers have which pieces
	peers map[*peer.Peer]*Peer
	// protects peersByPiece, peers, remaining and requested fields
	m sync.Mutex
	// will be closed by main loop when all of the remaining pieces are downloaded
	finished chan struct{}
	log      logger.Logger
}

type Peer struct {
	pieces       []uint32
	haveNewPiece chan struct{}
}

type Transfer interface {
	InfoHash() protocol.InfoHash
	NumPieces() uint32
	Piece(i uint32) *piece.Piece
	PieceLength(i uint32) uint32
	PieceOK(i uint32) bool
	Downloader() peer.Downloader
	Uploader() peer.Uploader
	Finished() chan struct{}
}

func New(t Transfer, port uint16, peerID protocol.PeerID, enableEncryption, forceEncryption bool) *Downloader {
	remaining := make(map[uint32]struct{})
	for i := uint32(0); i < t.NumPieces(); i++ {
		if !t.Piece(i).OK() {
			remaining[i] = struct{}{}
		}
	}
	peers := make([]map[*peer.Peer]struct{}, t.NumPieces())
	for i := range peers {
		peers[i] = make(map[*peer.Peer]struct{})
	}
	return &Downloader{
		transfer:         t,
		port:             port,
		peerID:           peerID,
		enableEncryption: enableEncryption,
		forceEncryption:  forceEncryption,
		remaining:        remaining,
		requested:        make(map[uint32]time.Time),
		peersC:           make(chan []*net.TCPAddr),
		peerC:            make(chan *net.TCPAddr, maxPeerPerTorrent),
		completeC:        make(chan uint32),
		pieceC:           make(chan *peer.PieceMessage),
		cancelC:          make(chan struct{}),
		haveC:            make(chan *peer.HaveMessage),
		peersByPiece:     peers,
		peers:            make(map[*peer.Peer]*Peer),
		finished:         make(chan struct{}),
		log:              logger.New("downloader "),
	}
}

func (d *Downloader) PeersC() chan []*net.TCPAddr     { return d.peersC }
func (d *Downloader) PieceC() chan *peer.PieceMessage { return d.pieceC }
func (d *Downloader) HaveC() chan *peer.HaveMessage   { return d.haveC }
func (d *Downloader) Finished() chan struct{}         { return d.finished }

func (d *Downloader) Run() {
	d.log.Debug("started downloader")

	left := len(d.remaining)

	go d.connecter()
	go d.peerManager()
	go d.haveManager()

	for {
		select {
		case <-d.completeC:
			left--
			if left == 0 {
				d.log.Notice("Download finished.")
				close(d.transfer.Finished())
				return // TODO remove this?
			}
		case <-d.cancelC:
			return
		}
	}
}

// haveManager receives have messages from peers and maintains d.peers and d.pieces maps.
func (d *Downloader) haveManager() {
	for {
		select {
		case have := <-d.haveC:
			// update d.peers and d.pieces maps
			d.m.Lock()
			d.peersByPiece[have.Index][have.Peer] = struct{}{}
			peer := d.peers[have.Peer]
			peer.pieces = append(peer.pieces, have.Index)

			// remove from maps when peer disconnects
			go func() {
				<-have.Peer.Disconnected
				d.m.Lock()
				delete(d.peersByPiece[have.Index], have.Peer)
				d.m.Unlock()
			}()

			// notify paused downloaders
			select {
			case peer.haveNewPiece <- struct{}{}:
			default:
			}

			d.m.Unlock()
		case <-d.cancelC:
			return
		}
	}
}

// peerManager receives from d.peersC and keeps most recent peer addresses in d.peerC.
func (d *Downloader) peerManager() {
	for {
		select {
		case <-d.cancelC:
			return
		case peers := <-d.peersC:
			for _, p := range peers {
				d.log.Debug("Peer:", p)
				// Try to put the peer into d.peerC
				select {
				case d.peerC <- p:
				case <-d.cancelC:
					return
				default:
					// If the channel is full,
					// discard a message from the channel...
					select {
					case <-d.peerC:
					case <-d.cancelC:
						return
					default:
						break
					}
					// ... and try to put it again.
					select {
					case d.peerC <- p:
					case <-d.cancelC:
						return
					// If channel is still full, give up.
					default:
						break
					}
				}
			}
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

	d.m.Lock()
	d.peers[p] = &Peer{make([]uint32, 0), make(chan struct{}, 1)}
	d.m.Unlock()
	go func() {
		<-p.Disconnected
		d.m.Lock()
		delete(d.peers, p)
		d.m.Unlock()
	}()
	go d.peerDownloader(p)
	p.Run()
}

func (d *Downloader) peerDownloader(peer *peer.Peer) {
	var interested bool
	for {
		d.m.Lock()
		candidates := d.candidates(peer)
		if len(candidates) == 0 {
			p := d.peers[peer]
			d.m.Unlock()
			select {
			case <-p.haveNewPiece:
				// Do not try to select piece on first "have" message. Wait for more messages for better selection.
				time.Sleep(time.Second)
				continue
			case <-peer.Disconnected:
				return
			}
		}
		selected := d.selectPiece(candidates)
		delete(d.remaining, selected)
		d.requested[selected] = time.Now()
		d.m.Unlock()

		if !interested {
			peer.SendInterested()
			interested = true
		} else if interested {
			peer.SendNotInterested()
			interested = false
		}

		piece := d.transfer.Piece(selected)

		// TODO queue max 10 requests

		// Request blocks of the piece.
		for _, b := range piece.Blocks() {
			if err := peer.Request(selected, b.Begin, b.Length); err != nil {
				d.log.Error(err)
				return
			}
		}

		// Read blocks from peer.
		pieceData := make([]byte, piece.Length())
		for _ = range piece.Blocks() { // TODO all peers send to this channel
			select {
			case peerBlock := <-d.pieceC:
				data := <-peerBlock.Data
				if data == nil {
					d.log.Error("peer did not send block completely")
					return
				}
				d.log.Debugln("Will receive block of length", len(data))
				copy(pieceData[peerBlock.Begin:], data)
			case <-time.After(16 * time.Second): // speed is below 1KBps
				d.log.Error("piece timeout")
				return
			}
		}

		if _, err := piece.Write(pieceData); err != nil {
			d.log.Error(err)
			peer.Close()
			return
		}

		d.m.Lock()
		delete(d.requested, selected)
		select {
		case d.completeC <- selected:
		case <-peer.Disconnected:
			return
		}
		d.m.Unlock()
	}
}

// candidates returns list of piece indexes which is available on the peer but not available on the client.
func (d *Downloader) candidates(p *peer.Peer) (candidates []uint32) {
	for _, i := range d.peers[p].pieces {
		if _, ok := d.remaining[i]; ok {
			candidates = append(candidates, i)
		}
	}
	return
}

// selectPiece returns the index of the selected piece from candidates.
func (d *Downloader) selectPiece(candidates []uint32) uint32 {
	var pieces []pieceWithAvailability
	for _, i := range candidates {
		pieces = append(pieces, pieceWithAvailability{i, len(d.peersByPiece[i])})
	}
	sort.Sort(rarestFirst(pieces))
	minRarity := pieces[0].availability
	var rarestPieces []uint32
	for _, r := range pieces {
		if r.availability > minRarity {
			break
		}
		rarestPieces = append(rarestPieces, r.index)
	}
	return rarestPieces[rand.Intn(len(rarestPieces))]
}

type pieceWithAvailability struct {
	index        uint32
	availability int
}

// rarestFirst implements sort.Interface based on availability of piece.
type rarestFirst []pieceWithAvailability

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return r[i].availability < r[j].availability }

// var errNoPiece = errors.New("no piece available for download")
// var errNoPeer = errors.New("no peer available for this piece")
