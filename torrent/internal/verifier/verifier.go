package verifier

import (
	"crypto/sha1" // nolint: gosec

	"github.com/cenkalti/rain/torrent/internal/bitfield"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Verifier struct {
	pieces []pieceio.Piece

	progressC chan Progress
	resultC   chan Result

	stopC chan struct{}
	doneC chan struct{}
}

type Progress struct {
	Checked uint32
	OK      uint32
}

type Result struct {
	Bitfield *bitfield.Bitfield
	Error    error
}

func New(pieces []pieceio.Piece, progressC chan Progress, resultC chan Result) *Verifier {
	return &Verifier{
		pieces:    pieces,
		progressC: progressC,
		resultC:   resultC,
		stopC:     make(chan struct{}),
		doneC:     make(chan struct{}),
	}
}

func (v *Verifier) Stop() {
	close(v.stopC)
}

func (v *Verifier) Done() chan struct{} {
	return v.doneC
}

func (v *Verifier) Run() {
	defer close(v.doneC)

	result := Result{
		Bitfield: bitfield.New(uint32(len(v.pieces))),
	}
	defer func() {
		select {
		case v.resultC <- result:
		case <-v.stopC:
		}
	}()

	buf := make([]byte, v.pieces[0].Length)
	hash := sha1.New() // nolint: gosec
	var numOK uint32
	for _, p := range v.pieces {
		buf = buf[:p.Length]
		_, result.Error = p.Data.ReadAt(buf, 0)
		if result.Error != nil {
			return
		}
		ok := p.VerifyHash(buf, hash)
		if ok {
			result.Bitfield.Set(p.Index)
			numOK++
		}
		select {
		case v.progressC <- Progress{Checked: p.Index + 1, OK: numOK}:
		case <-v.stopC:
			return
		}
		hash.Reset()
	}
}
