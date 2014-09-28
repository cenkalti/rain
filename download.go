package rain

import (
	"math/rand"
	"net"
	"runtime"
	"sort"
	"time"

	"github.com/cenkalti/rain/internal/connection"
	"github.com/cenkalti/rain/internal/logger"
)

// peerManager receives from t.peersC and keeps most recent peer addresses in t.peerC.
func (t *transfer) peerManager() {
	t.log.Debug("Started peerManager")
	for {
		select {
		case <-t.stopC:
			return
		case peers := <-t.peersC:
			for _, p := range peers {
				t.log.Debugln("Peer:", p)
				go func(addr *net.TCPAddr) {
					select {
					case t.peerC <- addr:
					case <-t.stopC:
					}
				}(p)
			}
		}
	}
}

// connecter connects to peers coming from t. peerC.
func (t *transfer) connecter() {
	limit := make(chan struct{}, maxPeerPerTorrent)
	for {
		select {
		case p := <-t.peerC:
			if p.Port == 0 {
				break
			}
			if p.IP.IsLoopback() && p.Port == int(t.rain.Port()) {
				break
			}

			limit <- struct{}{}
			go func(addr *net.TCPAddr) {
				defer func() {
					if err := recover(); err != nil {
						buf := make([]byte, 10000)
						t.log.Critical(err, "\n", string(buf[:runtime.Stack(buf, false)]))
					}
					<-limit
				}()
				t.connect(addr)
			}(p)
		case <-t.stopC:
			return
		}
	}
}

func (t *transfer) connect(addr *net.TCPAddr) {
	log := logger.New("peer -> " + addr.String())

	conn, cipher, extensions, peerID, err := connection.Dial(addr, !t.rain.config.Encryption.DisableOutgoing, t.rain.config.Encryption.ForceOutgoing, [8]byte{}, t.torrent.Info.Hash, t.rain.peerID)
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

	p := t.newPeer(conn, peerID, log)

	t.m.Lock()
	t.peers[peerID] = p
	t.m.Unlock()
	go func() {
		<-p.Disconnected
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	if err = p.SendBitfield(); err != nil {
		log.Error(err)
		return
	}

	go p.downloader()
	p.Run()
}

func (peer *Peer) downloader() {
	t := peer.transfer
	for {
		t.m.Lock()
		if t.bitfield.All() {
			t.onceFinished.Do(func() { close(t.finished) })
			t.m.Unlock()
			return
		}
		candidates := peer.candidates()
		if len(candidates) == 0 {
			t.m.Unlock()

			peer.BeNotInterested()
			select {
			case <-peer.haveNewPiece:
				continue
			case <-peer.Disconnected:
				return
			}
		}
		piece := t.selectPiece(candidates)
		t.m.Unlock()

		peer.BeInterested()

		peer.chokeCond.L.Lock()
		for peer.peerChoking {
			peer.chokeCond.Wait()
		}
		peer.chokeCond.L.Unlock()

		// TODO queue max 10 requests

		// Request blocks of the piece.
		for _, b := range piece.Blocks {
			if err := peer.Request(piece.Index, b.Begin, b.Length); err != nil {
				t.log.Error(err)
				return
			}
		}

		// Read blocks from peer.
		pieceData := make([]byte, piece.Length)
		for _ = range piece.Blocks { // TODO all peers send to this channel
			select {
			case peerBlock := <-peer.pieceC:
				data := <-peerBlock.Data
				if data == nil {
					t.log.Error("peer did not send block completely")
					return
				}
				t.log.Debugln("Will receive block of length", len(data))
				copy(pieceData[peerBlock.Begin:], data)
			case <-time.After(16 * time.Second): // speed is below 1KBps
				t.log.Error("piece timeout")
				return
			}
		}

		if _, err := piece.Write(pieceData); err != nil {
			t.log.Error(err)
			peer.Close()
			return
		}

		t.m.Lock()
		t.bitfield.Set(piece.Index)
		t.m.Unlock()
	}
}

// candidates returns list of piece indexes which is available on the peer but not available on the client.
func (p *Peer) candidates() (candidates []*Piece) {
	p.m.Lock()
	for i := uint32(0); i < p.transfer.bitfield.Len(); i++ {
		if !p.transfer.bitfield.Test(i) && p.bitfield.Test(i) {
			candidates = append(candidates, p.transfer.pieces[i])
		}
	}
	p.m.Unlock()
	return
}

// selectPiece returns the index of the selected piece from candidates.
func (t *transfer) selectPiece(candidates []*Piece) *Piece {
	sort.Sort(rarestFirst(candidates))
	minAvailability := candidates[0].availability()
	var i int
	for _, piece := range candidates {
		if piece.availability() > minAvailability {
			break
		}
		i++
	}
	candidates = candidates[:i]
	return candidates[rand.Intn(len(candidates))]
}

// rarestFirst implements sort.Interface based on availability of piece.
type rarestFirst []*Piece

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return r[i].availability() < r[j].availability() }

// var errNoPiece = errors.New("no piece available for download")
// var errNoPeer = errors.New("no peer available for this piece")
