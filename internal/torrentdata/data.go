package torrentdata

import (
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/pieceio"
	"github.com/cenkalti/rain/storage"
	"github.com/hashicorp/go-multierror"
)

type Data struct {
	Pieces []pieceio.Piece
	Exists bool
	files  []storage.File
}

func New(info *metainfo.Info, sto storage.Storage) (*Data, error) {
	files, exists, err := prepareFiles(info, sto)
	if err != nil {
		return nil, err
	}
	pieces := pieceio.NewPieces(info, files)
	return &Data{
		Pieces: pieces,
		files:  files,
		Exists: exists,
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
