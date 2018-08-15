package torrentdata

import (
	"os"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/piece"
)

type Data struct {
	Pieces    []piece.Piece
	files     []*os.File
	bitfield  *bitfield.Bitfield // keeps track of the pieces we have
	checkHash bool
	Completed chan struct{}
}

func New(info *metainfo.Info, dest string) (*Data, error) {
	files, checkHash, err := prepareFiles(info, dest)
	if err != nil {
		return nil, err
	}
	pieces := piece.NewPieces(info, files)
	return &Data{
		Pieces:    pieces,
		files:     files,
		bitfield:  bitfield.New(uint32(len(pieces))),
		checkHash: checkHash,
		Completed: make(chan struct{}),
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
	if !d.checkHash {
		return nil
	}
	for i, p := range d.Pieces {
		if err := p.Verify(); err != nil {
			return err
		}
		d.bitfield.SetTo(uint32(i), p.OK)
	}
	return nil
}
