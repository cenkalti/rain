package torrentdata

import (
	"crypto/sha1" // nolint: gosec
	"os"
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/piece"
)

type Data struct {
	Pieces        []piece.Piece
	files         []*os.File
	bitfield      *bitfield.Bitfield // keeps track of the pieces we have
	checkHash     bool
	pieceLength   uint32
	completeC     chan struct{}
	onceCompleted sync.Once // for closing completed channel only once
}

func New(info *metainfo.Info, dest string, completeC chan struct{}) (*Data, error) {
	files, checkHash, err := prepareFiles(info, dest)
	if err != nil {
		return nil, err
	}
	pieces := piece.NewPieces(info, files)
	return &Data{
		Pieces:      pieces,
		files:       files,
		bitfield:    bitfield.New(uint32(len(pieces))),
		checkHash:   checkHash,
		pieceLength: info.PieceLength,
		completeC:   completeC,
	}, nil
}

func (d *Data) Bitfield() *bitfield.Bitfield {
	return d.bitfield
}

func (d *Data) Close() error {
	var result error
	for _, f := range d.files {
		err := f.Close()
		if err != nil {
			result = multierror.Append(result, err)
		}
	}
	return result
}

func (d *Data) Verify() error {
	// TODO set bitfield bits according to resume data
	if !d.checkHash {
		return nil
	}
	buf := make([]byte, d.pieceLength)
	hash := sha1.New() // nolint: gosec
	for _, p := range d.Pieces {
		err := p.Data.ReadFull(buf)
		if err != nil {
			return err
		}
		ok := p.VerifyHash(buf[:p.Length], hash)
		d.bitfield.SetTo(p.Index, ok)
		hash.Reset()
	}
	d.CheckCompletion()
	return nil
}

func (d *Data) CheckCompletion() {
	if d.Bitfield().All() {
		d.onceCompleted.Do(func() {
			close(d.completeC)
		})
	}
}
