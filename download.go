package rain

import (
	"math/rand"
	"net"
	"runtime"
	"sort"
	"sync"
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
				// TODO send all peers to connecter for now
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
			// 0 port is invalid
			if p.Port == 0 {
				break
			}
			// Do not connect yourself
			if p.IP.IsLoopback() && p.Port == int(t.client.Port()) {
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
				t.connectAndRun(addr)
			}(p)
		case <-t.stopC:
			return
		}
	}
}

func (t *transfer) connectAndRun(addr *net.TCPAddr) {
	log := newLogger("peer -> " + addr.String())

	conn, cipher, extensions, peerID, err := dial(addr, !t.client.config.Encryption.DisableOutgoing, t.client.config.Encryption.ForceOutgoing, [8]byte{}, t.torrent.Info.Hash, t.client.peerID)
	if err != nil {
		if err == errOwnConnection {
			log.Debug(err)
		} else {
			log.Error(err)
		}
		return
	}
	log.Infof("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, extensions, peerID[:8])
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

func (peer *peer) downloader() {
	t := peer.transfer
	for {
		// Select next piece to download.
		t.m.Lock()
		var candidates []*piece
		var waitNotInterested sync.WaitGroup
		for candidates = peer.candidates(); len(candidates) == 0 && !peer.disconnected; {
			// Stop downloader if all pieces are downloaded.
			if t.bitfield.All() {
				t.onceFinished.Do(func() {
					close(t.finished)
					t.log.Notice("Download completed")
				})
				t.m.Unlock()
				return
			}

			// Send "not interesed" message in a goroutine here because we can't keep the mutex locked.
			waitNotInterested.Add(1)
			go func() {
				peer.BeNotInterested()
				waitNotInterested.Done()
			}()

			// Wait until there is a piece that we are interested.
			peer.cond.Wait()
		}
		if peer.disconnected {
			return
		}
		piece := selectPiece(candidates)
		request := piece.createActiveRequest(peer.id)
		t.m.Unlock()

		// send them in order
		waitNotInterested.Wait()
		peer.BeInterested()

		for {
			// Stop loop if all blocks are requested/received TODO.
			t.m.Lock()
			if piece.requestedFrom[peer.id].blocksReceived.All() {
				t.m.Unlock()
				break
			}

			// Send requests only when unchoked.
			for peer.peerChoking && !peer.disconnected {
				request.resetWaitingRequests()
				peer.cond.Wait()
			}

			// Select next block that is not requested.
			block, ok := piece.nextBlock(peer.id)
			if !ok {
				t.m.Unlock()
				break
			}
			request.blocksRequesting.Set(block.Index)
			t.m.Unlock()

			// Request selected block.
			if err := peer.Request(block); err != nil {
				peer.log.Error(err)
				t.m.Lock()
				piece.deleteActiveRequest(peer.id)
				t.m.Unlock()
				return
			}

			t.m.Lock()
			request.blocksRequested.Set(block.Index)
			for request.outstanding() >= 10 {
				peer.cond.Wait()
			}
			t.m.Unlock()
		}
		// TODO handle choke while receiving pieces. Re-reqeust, etc..
	}
}

// candidates returns list of piece indexes which is available on the peer but not available on the client.
func (p *peer) candidates() (candidates []*piece) {
	for i := uint32(0); i < p.transfer.bitfield.Len(); i++ {
		if !p.transfer.bitfield.Test(i) && p.bitfield.Test(i) {
			piece := p.transfer.pieces[i]
			if _, ok := piece.requestedFrom[p.id]; !ok {
				candidates = append(candidates, piece)
			}
		}
	}
	return
}

// selectPiece returns the index of the selected piece from candidates.
func selectPiece(candidates []*piece) *piece {
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
type rarestFirst []*piece

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return r[i].availability() < r[j].availability() }
