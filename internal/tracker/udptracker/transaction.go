package udptracker

import (
	"context"
	"io"
	"math/rand"
)

type transaction struct {
	id int32

	// This can be a connection or announce request
	request udpRequest

	// Child context of the request.
	// Transaction has it's own sub-context.
	// This context will be closed by run loop to signal waiters that the transaction is finished.
	// Either successful or with error.
	ctx    context.Context
	cancel func()
}

type udpRequest interface {
	io.WriterTo
	SetTransactionID(int32)
	GetContext() context.Context
	GetResponse() (data []byte, err error)
	SetResponse(data []byte, err error)
}

func newTransaction(req udpRequest) *transaction {
	t := &transaction{
		id:      rand.Int31(), // nolint: gosec
		request: req,
	}
	req.SetTransactionID(t.id)
	t.ctx, t.cancel = context.WithCancel(req.GetContext())
	return t
}
