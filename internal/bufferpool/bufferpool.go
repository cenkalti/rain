package bufferpool

import "sync"

// Pool is a wrapper around sync.Pool with a helper Release method on returned objects.
// Objects in the Pool are Buffers which are wrapper of a slice with a pointer to the Pool object.
type Pool struct {
	pool sync.Pool
}

// New returns a new Pool for Buffers of size buflen.
func New(buflen int) *Pool {
	return &Pool{
		pool: sync.Pool{
			New: func() interface{} {
				b := make([]byte, buflen)
				return &b
			},
		},
	}
}

// Get a new Buffer from the pool. datalen must not exceed buffer length given in constructor.
// You should release the Buffer after your work is done by calling Buffer.Release.
func (p *Pool) Get(datalen int) Buffer {
	buf := p.pool.Get().(*[]byte)
	return newBuffer(buf, datalen, p)
}

// Buffer is a slice with a pointer to Pool.
type Buffer struct {
	Data []byte
	buf  *[]byte
	pool *Pool
}

func newBuffer(buf *[]byte, length int, pool *Pool) Buffer {
	return Buffer{
		Data: (*buf)[:length],
		buf:  buf,
		pool: pool,
	}
}

// Release the Buffer and return it to the Pool.
func (b Buffer) Release() {
	// argument to Put should be pointer-like to avoid allocations
	b.pool.pool.Put(b.buf)
}
