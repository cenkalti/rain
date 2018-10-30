package verifier

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Verifier struct {
	Bitfield *bitfield.Bitfield
	Error    error

	closeC chan struct{}
	doneC  chan struct{}
}

type Progress struct {
	Checked uint32
	OK      uint32
}

func New() *Verifier {
	return &Verifier{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

func (v *Verifier) Close() {
	close(v.closeC)
	<-v.doneC
}

func (v *Verifier) Run(pieces []pieceio.Piece, progressC chan Progress, resultC chan *Verifier) {
	defer close(v.doneC)

	defer func() {
		select {
		case resultC <- v:
		case <-v.closeC:
		}
	}()

	v.Bitfield = bitfield.New(uint32(len(pieces)))
	buf := make([]byte, pieces[0].Length)
	hash := sha1.New() // nolint: gosec
	var numOK uint32
	for _, p := range pieces {
		buf = buf[:p.Length]
		_, v.Error = p.Data.ReadAt(buf, 0)
		if v.Error != nil {
			return
		}
		ok := p.VerifyHash(buf, hash)
		if ok {
			v.Bitfield.Set(p.Index)
			numOK++
		}
		select {
		case progressC <- Progress{Checked: p.Index + 1, OK: numOK}:
		case <-v.closeC:
			return
		}
		hash.Reset()
	}
}
