package verifier

import (
	"crypto/sha1"
	"errors"

	"github.com/cenkalti/rain/v2/internal/bitfield"
	"github.com/cenkalti/rain/v2/internal/piece"
	"github.com/cenkalti/rain/v2/storage"
)

// Verifier verifies the pieces on disk.
type Verifier struct {
	Bitfield *bitfield.Bitfield
	Error    error

	closeC chan struct{}
	doneC  chan struct{}
}

// Progress information about the verification.
type Progress struct {
	Checked uint32
}

// New returns a new Verifier.
func New() *Verifier {
	return &Verifier{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the verifier.
func (v *Verifier) Close() {
	close(v.closeC)
	<-v.doneC
}

// Run and verify all pieces of the torrent.
func (v *Verifier) Run(pieces []piece.Piece, progressC chan Progress, resultC chan *Verifier) {
	defer close(v.doneC)

	defer func() {
		select {
		case resultC <- v:
		case <-v.closeC:
		}
	}()

	v.Bitfield = bitfield.New(uint32(len(pieces)))
	buf := make([]byte, pieces[0].Length)
	hash := sha1.New()
	var numOK uint32
	for _, p := range pieces {
		buf = buf[:p.Length]
		_, err := p.Data.ReadAt(buf, 0)
		if err != nil && !errors.Is(err, storage.ErrNotWritten) {
			v.Error = err
			return
		}
		if err == nil && p.VerifyHash(buf, hash) {
			v.Bitfield.Set(p.Index)
			numOK++
		}
		select {
		case progressC <- Progress{Checked: p.Index + 1}:
		case <-v.closeC:
			return
		}
		hash.Reset()
	}
}
