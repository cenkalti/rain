package rain

import (
	"errors"
	"math/rand"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/tracker"
)

type downloader struct {
	transfer  *transfer
	remaining []*piece
	peersC    chan []tracker.Peer
	peerC     chan tracker.Peer
	requestC  chan chan *piece
	responseC chan *piece
	cancelC   chan struct{}
	log       logger.Logger
}

func newDownloader(t *transfer) *downloader {
	remaining := make([]*piece, 0, len(t.pieces))
	for i := range t.pieces {
		if !t.pieces[i].ok {
			remaining = append(remaining, t.pieces[i])
		}
	}
	return &downloader{
		transfer:  t,
		remaining: remaining,
		peersC:    make(chan []tracker.Peer),
		peerC:     make(chan tracker.Peer, tracker.NumWant),
		requestC:  make(chan chan *piece),
		responseC: make(chan *piece),
		cancelC:   make(chan struct{}),
		log:       t.log,
	}
}

func (d *downloader) Run() {
	t := d.transfer
	t.log.Debug("started downloader")

	left := len(d.remaining)

	go d.connecter()
	go d.peerManager()
	go d.pieceRequester()

	// Download pieces in parallel.
	for i := 0; i < simultaneoutPieceDownload; i++ {
		go d.pieceDownloader()
	}

	for {
		select {
		case p := <-d.responseC:
			t.bitField.Set(p.index) // #####
			left--
			if left == 0 {
				t.log.Notice("Finished")
				close(t.Finished)
				return
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
				d.log.Debug("Peer:", peer.TCPAddr())
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
			limit <- struct{}{}
			go func(peer tracker.Peer) {
				defer func() {
					if err := recover(); err != nil {
						d.transfer.log.Critical(err)
					}
					<-limit
				}()
				d.transfer.connectToPeer(peer.TCPAddr())
			}(p)
		case <-d.cancelC:
			return
		}
	}
}

// pieceRequester selects a piece to be downloaded next and sends it to d.requestC.
func (d *downloader) pieceRequester() {
	t := d.transfer
	const waitDuration = time.Second

	time.Sleep(waitDuration)
	for {
		req := make(chan *piece)
		select {
		case d.requestC <- req:
		case <-d.cancelC:
			return
		}

		t.haveCond.L.Lock()
		i, err := d.selectPiece()
		if err != nil {
			t.log.Debug(err)

			// Block until we have next "have" message
			t.haveCond.Wait()
			t.haveCond.L.Unlock()
			// Do not try to select piece on first "have" message. Wait for more messages for better selection.
			time.Sleep(waitDuration)
			continue
		}
		t.haveCond.L.Unlock()

		piece := d.remaining[i]
		piece.log.Debug("selected")

		// delete selected
		d.remaining[i], d.remaining = d.remaining[len(d.remaining)-1], d.remaining[:len(d.remaining)-1]

		select {
		case req <- piece:
		case <-d.cancelC:
			return
		}
	}
}

// pieceDownloader receives a piece from d.requestC and downloads it.
func (d *downloader) pieceDownloader() {
	for req := range d.requestC {
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

		d.responseC <- piece
	}
}

// selectPiece returns the index of the piece in pieces.
func (d *downloader) selectPiece() (int, error) {
	var pieces []t // pieces with peers
	for i, p := range d.remaining {
		p.peersM.Lock()
		if len(p.peers) > 0 {
			pieces = append(pieces, t{i, p})
		}
		p.peersM.Unlock()
	}
	if len(pieces) == 0 {
		return -1, errNoPiece
	}
	if len(pieces) == 1 {
		return 0, nil
	}
	sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))].i, nil
}

type t struct {
	i int
	p *piece
}

// Implements sort.Interface based on availability of piece.
type rarestFirst []t

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return len(r[i].p.peers) < len(r[j].p.peers) }

var errNoPiece = errors.New("no piece available for download")
var errNoPeer = errors.New("no peer available for this piece")

func (p *piece) selectPeer() (*peer, error) {
	p.peersM.Lock()
	defer p.peersM.Unlock()
	if len(p.peers) == 0 {
		return nil, errNoPeer
	}
	return p.peers[rand.Intn(len(p.peers))], nil
}

func (p *piece) download() error {
	p.log.Debug("downloading")

	peer, err := p.selectPeer()
	if err != nil {
		return err
	}
	p.log.Debugln("selected peer:", peer.conn.RemoteAddr())

	return peer.downloadPiece(p)
}
