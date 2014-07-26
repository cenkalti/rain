package rain

import (
	"errors"
	"math/rand"
	"sort"
	"time"
)

func (t *transfer) downloader() {
	t.log.Debug("started downloader")

	requestC := make(chan chan *piece)
	responseC := make(chan *piece)

	missing := t.bitField.Len() - t.bitField.Count()

	go t.pieceRequester(requestC)

	// Download pieces in parallel.
	for i := 0; i < simultaneoutPieceDownload; i++ {
		go pieceDownloader(requestC, responseC)
	}

	for p := range responseC {
		t.bitField.Set(p.index)

		missing--
		if missing == 0 {
			break
		}
	}

	t.log.Notice("Finished")
	close(t.Finished)
}

func (t *transfer) pieceRequester(requestC chan<- chan *piece) {
	const waitDuration = time.Second

	requested := make([]bool, t.bitField.Len())
	missing := t.bitField.Len() - t.bitField.Count()

	time.Sleep(waitDuration)
	for missing > 0 {
		req := make(chan *piece)
		requestC <- req

		t.haveCond.L.Lock()
		piece, err := t.selectPiece(requested)
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

		piece.log.Debug("selected")
		requested[piece.index] = true
		req <- piece
		missing--
	}
	close(requestC)
}

func pieceDownloader(requestC <-chan chan *piece, responseC chan<- *piece) {
	for req := range requestC {
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

		responseC <- piece
	}
}

func (t *transfer) selectPiece(requested []bool) (*piece, error) {
	var pieces []*piece
	for i, p := range t.pieces {
		p.peersM.Lock()
		if !requested[i] && !p.ok && len(p.peers) > 0 {
			pieces = append(pieces, p)
		}
		p.peersM.Unlock()
	}
	if len(pieces) == 0 {
		return nil, errPieceNotAvailable
	}
	if len(pieces) == 1 {
		return pieces[0], nil
	}
	sort.Sort(rarestFirst(pieces))
	pieces = pieces[:len(pieces)/2]
	return pieces[rand.Intn(len(pieces))], nil
}

// Implements sort.Interface based on availability of piece.
type rarestFirst []*piece

func (r rarestFirst) Len() int           { return len(r) }
func (r rarestFirst) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r rarestFirst) Less(i, j int) bool { return len(r[i].peers) < len(r[j].peers) }

var errPieceNotAvailable = errors.New("piece not available for download")

func (p *piece) selectPeer() (*peerConn, error) {
	p.peersM.Lock()
	defer p.peersM.Unlock()
	if len(p.peers) == 0 {
		return nil, errPieceNotAvailable
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
