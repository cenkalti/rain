package cachedpiece

import (
	"encoding/binary"

	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/piececache"
)

// CachedPiece is a wrapper around a piece.Piece object that is capable of reading the data from a picecache.Cache.
type CachedPiece struct {
	pi       *piece.Piece
	cache    *piececache.Cache
	readSize int64
	peerID   []byte
}

// New returns a new CachedPiece object. Reads are done with blocks of `readSize`.
func New(pi *piece.Piece, cache *piececache.Cache, readSize int64, peerID [20]byte) *CachedPiece {
	return &CachedPiece{
		pi:       pi,
		cache:    cache,
		readSize: readSize,
		peerID:   peerID[:],
	}
}

// ReadAt implements the io.ReaderAt interface.
func (c *CachedPiece) ReadAt(p []byte, off int64) (n int, err error) {
	blk := uint32(off / c.readSize)
	blkBegin := uint32(int64(blk) * c.readSize)
	blkEnd := uint32(int64(blkBegin) + c.readSize)
	if blkEnd > c.pi.Length {
		blkEnd = c.pi.Length
	}

	key := make([]byte, 20+4+4)
	copy(key, c.peerID)
	binary.BigEndian.PutUint32(key[20:24], c.pi.Index)
	binary.BigEndian.PutUint32(key[24:28], blk)

	buf, err := c.cache.Get(string(key), func() ([]byte, error) {
		b := make([]byte, blkEnd-blkBegin)
		_, err = c.pi.Data.ReadAt(b, int64(blkBegin))
		return b, err
	})
	if err != nil {
		return
	}

	begin := off - int64(blkBegin)
	return copy(p, buf[begin:]), nil
}
