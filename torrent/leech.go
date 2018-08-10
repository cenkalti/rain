// +build ignore

package torrent

import (
	"math/rand"
	"net"
	"sort"
	"sync"

	// "github.com/cenkalti/rain/btconn"
	// "github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/piece"
)

func (p *peer) downloader() {
	t := p.torrent
	for {
		// Select next piece to download.
		t.m.Lock()
		var candidates []*piece.Piece
		var waitNotInterested sync.WaitGroup
		for candidates = p.candidates(); len(candidates) == 0 && !p.disconnected; {
			// Stop downloader if all pieces are downloaded.
			if t.bitfield.All() {
				t.onceCompleted.Do(func() {
					close(t.completed)
					t.log.Notice("Download completed")
				})
				t.m.Unlock()
				return
			}

			// Send "not interesed" message in a goroutine here because we can't keep the mutex locked.
			waitNotInterested.Add(1)
			go func() {
				p.BeNotInterested() // nolint TODO
				waitNotInterested.Done()
			}()

			// Wait until there is a piece that we are interested.
			p.cond.Wait()
		}
		if p.disconnected {
			return
		}
		candidate := selectPiece(candidates)
		request := candidate.CreateRequest(p.id)
		t.m.Unlock()

		// send them in order
		waitNotInterested.Wait()
		p.BeInterested() // nolint TODO

		for {
			// Stop loop if all blocks are requested/received TODO.
			t.m.Lock()
			if candidate.RequestedFrom[p.id].BlocksReceived.All() {
				t.m.Unlock()
				break
			}

			// Send requests only when unchoked.
			for p.peerChoking && !p.disconnected {
				request.ResetWaitingRequests()
				p.cond.Wait()
			}

			// Select next block that is not requested.
			block, ok := candidate.NextBlock(p.id)
			if !ok {
				t.m.Unlock()
				break
			}
			request.BlocksRequesting.Set(block.Index)
			t.m.Unlock()

			// Request selected block.
			if err := p.Request(block); err != nil {
				p.log.Error(err)
				t.m.Lock()
				candidate.DeleteRequest(p.id)
				t.m.Unlock()
				return
			}

			t.m.Lock()
			request.BlocksRequested.Set(block.Index)
			for request.Outstanding() >= 10 {
				p.cond.Wait()
			}
			t.m.Unlock()
		}
		// TODO handle choke while receiving pieces. Re-reqeust, etc..
	}
}

// candidates returns list of piece indexes which is available on the peer but not available on the client.
func (p *peer) candidates() (candidates []*piece.Piece) {
	for i := uint32(0); i < p.torrent.bitfield.Len(); i++ {
		if !p.torrent.bitfield.Test(i) && p.bitfield.Test(i) {
			piece := p.torrent.pieces[i]
			if _, ok := piece.RequestedFrom[p.id]; !ok {
				candidates = append(candidates, piece)
			}
		}
	}
	return
}

// selectPiece returns the index of the selected piece from candidates.
func selectPiece(candidates []*piece.Piece) *piece.Piece {
	sort.Sort(rarestFirst(candidates))
	minAvailability := candidates[0].Availability()
	var i int
	for _, piece := range candidates {
		if piece.Availability() > minAvailability {
			break
		}
		i++
	}
	candidates = candidates[:i]
	return candidates[rand.Intn(len(candidates))]
}

// rarestFirst implements sort.Interface based on availability of piece.
type rarestFirst []*piece.Piece

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return r[i].Availability() < r[j].Availability() }
