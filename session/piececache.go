package session

import (
	"encoding/binary"
	"sync"

	"github.com/cenkalti/rain/session/internal/piece"
	"github.com/cenkalti/rain/session/internal/piececache"
)

type cachedPiece struct {
	pi       *piece.Piece
	cache    *piececache.Cache
	readSize int64
	m        *sync.Mutex
}

func (t *torrent) cachedPiece(pi *piece.Piece) *cachedPiece {
	return &cachedPiece{
		pi:       pi,
		cache:    t.pieceCache,
		readSize: t.config.PieceReadSize,
		m:        &t.readMutex,
	}
}

func (c *cachedPiece) ReadAt(p []byte, off int64) (n int, err error) {
	blk := uint32(off / c.readSize)
	blkBegin := uint32(int64(blk) * c.readSize)
	blkEnd := uint32(int64(blkBegin) + c.readSize)
	if blkEnd > c.pi.Length {
		blkEnd = c.pi.Length
	}

	key := make([]byte, 8)
	binary.BigEndian.PutUint32(key, c.pi.Index)
	binary.BigEndian.PutUint32(key[4:], blk)

	buf, err := c.cache.Get(string(key), func() ([]byte, error) {
		b := make([]byte, blkEnd-blkBegin)
		c.m.Lock()
		_, err = c.pi.Data.ReadAt(b, int64(blkBegin))
		c.m.Unlock()
		return b, err
	})
	if err != nil {
		return
	}

	begin := off - int64(blkBegin)
	return copy(p, buf[begin:]), nil
}
