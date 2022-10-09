package udptracker

import "context"

type requestBase struct {
	ctx  context.Context
	dest string

	response []byte
	err      error
	done     chan struct{}
}

func newRequestBase(ctx context.Context, dest string) *requestBase {
	return &requestBase{
		ctx:  ctx,
		dest: dest,
		done: make(chan struct{}),
	}
}

func (r *requestBase) GetContext() context.Context {
	return r.ctx
}

func (r *requestBase) GetResponse() (data []byte, err error) {
	return r.response, r.err
}

func (r *requestBase) SetResponse(data []byte, err error) {
	r.response, r.err = data, err
	close(r.done)
}
