package torrentdata

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"os"
	"sync"

	"github.com/hashicorp/go-multierror"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/metainfo"
	"github.com/cenkalti/rain/internal/piece"
)

type Data struct {
	Pieces        []piece.Piece
	hashes        [][]byte
	files         []*os.File
	bitfield      *bitfield.Bitfield // keeps track of the pieces we have
	checkHash     bool
	pieceLength   uint32
	Completed     chan struct{}
	onceCompleted sync.Once // for closing completed channel only once
}

func New(info *metainfo.Info, dest string) (*Data, error) {
	files, checkHash, err := prepareFiles(info, dest)
	if err != nil {
		return nil, err
	}
	pieces := piece.NewPieces(info, files)
	return &Data{
		Pieces:      pieces,
		hashes:      info.PieceHashes,
		files:       files,
		bitfield:    bitfield.New(uint32(len(pieces))),
		checkHash:   checkHash,
		pieceLength: info.PieceLength,
		Completed:   make(chan struct{}),
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

func (d *Data) ReadPiece(index, begin uint32, buf []byte) error {
	return d.Pieces[index].Data.ReadAt(buf, int64(begin))
}

func (d *Data) WritePiece(index uint32, buf []byte) error {
	p := d.Pieces[index]
	hash := sha1.New()
	hash.Write(buf)
	sum := hash.Sum(nil)
	ok := bytes.Equal(sum, d.hashes[index])
	if !ok {
		return errors.New("corrupt piece")
	}
	_, err := p.Data.Write(buf)
	if err != nil {
		return err
	}
	d.bitfield.Set(p.Index)
	d.checkCompletion()
	return nil
}

func (d *Data) Verify() error {
	if !d.checkHash {
		return nil
	}
	buf := make([]byte, d.pieceLength)
	hash := sha1.New()
	for i, p := range d.Pieces {
		err := p.Data.ReadFull(buf)
		if err != nil {
			return err
		}
		hash.Write(buf[:p.Length])
		sum := hash.Sum(nil)
		ok := bytes.Equal(sum, d.hashes[i])
		d.bitfield.SetTo(p.Index, ok)
		hash.Reset()
	}
	d.checkCompletion()
	return nil
}

func (d *Data) checkCompletion() {
	if d.Bitfield().All() {
		d.onceCompleted.Do(func() {
			close(d.Completed)
		})
	}
}
