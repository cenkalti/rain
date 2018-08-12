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
	checkHash bool
}

func New(info *metainfo.Info, dest string) (*Data, error) {
	files, checkHash, err := prepareFiles(info, dest)
	if err != nil {
		return nil, err
	}
	return &Data{
		files:     files,
		Pieces:    piece.NewPieces(info, files),
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

func (d *Data) Verify() (*bitfield.Bitfield, error) {
	b := bitfield.New(uint32(len(d.Pieces)))
	if d.checkHash {
		for i, p := range d.Pieces {
			if err := p.Verify(); err != nil {
				return nil, err
			}
			b.SetTo(uint32(i), p.OK)
		}
	}
	return b, nil
}
