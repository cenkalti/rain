package torrentdata

import (
	"github.com/cenkalti/rain/torrent/internal/metainfo"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
	"github.com/cenkalti/rain/torrent/storage"
	"github.com/hashicorp/go-multierror"
)

type Data struct {
	Pieces []pieceio.Piece
	files  []storage.File
}

func New(info *metainfo.Info, files []storage.File) *Data {
	pieces := pieceio.NewPieces(info, files)
	return &Data{
		Pieces: pieces,
		files:  files,
	}
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
