package bufferpool

import "sync"

type Pool struct {
	pool sync.Pool
}

func New(buflen int) *Pool {
	return &Pool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, buflen)
			},
		},
	}
}

func (p *Pool) Get(datalen int) Buffer {
	buf := p.pool.Get().([]byte)
	return newBuffer(buf, datalen, p)
}

type Buffer struct {
	Data []byte
	buf  []byte
	pool *Pool
}

func newBuffer(buf []byte, length int, pool *Pool) Buffer {
	return Buffer{
		Data: buf[:length],
		buf:  buf,
		pool: pool,
	}
}

func (b Buffer) Release() {
	b.pool.pool.Put(b.buf)
}
