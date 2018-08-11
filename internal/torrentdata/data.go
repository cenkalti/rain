package torrentdata

import (
	"os"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/piece"

	"github.com/hashicorp/go-multierror"
)

type Data struct {
	files     []*os.File
	pieces    []piece.Piece
	checkHash bool
}

func New(info *metainfo.Info, dest string) (*Data, error) {
	files, checkHash, err := prepareFiles(info, dest)
	if err != nil {
		return nil, err
	}
	return &Data{
		files:     files,
		pieces:    piece.NewPieces(info, files),
		checkHash: checkHash,
	}, nil
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

func (d *Data) Piece(index int) *piece.Piece {
	return &d.pieces[index]
}

func (d *Data) Verify() (*bitfield.Bitfield, error) {
	b := bitfield.New(uint32(len(d.pieces)))
	if d.checkHash {
		for _, p := range d.pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			b.SetTo(p.Index, p.OK)
		}
	}
	return b, nil
}
