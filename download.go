package rain

import (
	"errors"
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
	defer func() {
		t.m.Lock()
		delete(t.peers, peerID)
		t.m.Unlock()
	}()

	if err = p.SendBitfield(); err != nil {
		log.Error(err)
		return
	}

	p.Run()
}

func (p *Peer) downloader() {
	t := p.transfer
	for {
		t.m.Lock()
		if t.bitfield.All() {
			t.onceFinished.Do(func() { close(t.finished) })
			t.m.Unlock()
			return
		}
		candidates := p.candidates()
		if len(candidates) == 0 {
			t.m.Unlock()
			if err := p.BeNotInterested(); err != nil {
				p.log.Error(err)
				return
			}
			if err := p.waitForHaveMessage(); err != nil {
				p.log.Error(err)
				return
			}
			continue
		}
		piece := selectPiece(candidates)
		// Save selected piece so other downloaders do not try to download the same piece.
		// TODO remove from requests when downloader exited with error.
		t.requests[piece.Index] = &pieceRequest{p, time.Now()}
		t.m.Unlock()

		if err := p.BeInterested(); err != nil {
			p.log.Error(err)
			return
		}

		// TODO queue max 10 requests
		go func() {
			if err := p.requestBlocks(piece); err != nil {
				p.log.Error(err)
			}
		}()

		// TODO handle choke while receiving pieces. Re-reqeust, etc..

		// Read blocks from peer.
		pieceData := make([]byte, piece.Length)
		for i := 0; i < len(piece.Blocks); i++ {
			peerBlock := <-p.pieceC
			data := <-peerBlock.Data
			if data == nil {
				p.log.Error("peer did not send block completely")
				return
			}
			p.log.Debugln("Will receive block of length", len(data))
			copy(pieceData[peerBlock.Begin:], data)
		}

		if _, err := piece.Write(pieceData); err != nil {
			t.log.Error(err)
			p.Close()
			return
		}

		t.m.Lock()
		t.bitfield.Set(piece.Index)
		delete(t.requests, piece.Index)
		t.m.Unlock()
	}
}

func (p *Peer) requestBlocks(piece *Piece) error {
	for _, b := range piece.Blocks {
		// Send requests only when unchoked.
		if err := p.waitForUnchoke(); err != nil {
			return err
		}
		if err := p.Request(piece.Index, b.Begin, b.Length); err != nil {
			return err
		}
	}
	return nil
}

func (p *Peer) waitForHaveMessage() error {
	p.m.Lock()
	defer p.m.Unlock()
	count := p.bitfield.Count()
	for count == p.bitfield.Count() && !p.disconnected {
		p.cond.Wait()
	}
	if p.disconnected {
		return errors.New("disconnected while waiting for new have message")
	}
	return nil
}

func (p *Peer) waitForUnchoke() error {
	p.m.Lock()
	defer p.m.Unlock()
	for p.peerChoking && !p.disconnected {
		p.cond.Wait()
	}
	if p.disconnected {
		return errors.New("disconnected while waiting for unchoke message")
	}
	return nil
}

// candidates returns list of piece indexes which is available on the peer but not available on the client.
func (p *Peer) candidates() (candidates []*Piece) {
	p.m.Lock()
	for i := uint32(0); i < p.transfer.bitfield.Len(); i++ {
		if !p.transfer.bitfield.Test(i) && p.bitfield.Test(i) {
			piece := p.transfer.pieces[i]
			if _, ok := p.transfer.requests[piece.Index]; !ok {
				candidates = append(candidates, piece)
			}
		}
	}
	p.m.Unlock()
	return
}

// selectPiece returns the index of the selected piece from candidates.
func selectPiece(candidates []*Piece) *Piece {
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
