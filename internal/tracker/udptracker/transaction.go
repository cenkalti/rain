package udptracker

import (
	"math/rand"
	"net"
)

type transaction struct {
	request  udpRequest
	dest     string
	addr     net.Addr
	response []byte
	err      error
	done     chan struct{}
}

func newTransaction(req udpRequest, dest string) *transaction {
	req.SetTransactionID(rand.Int31())
	return &transaction{
		request: req,
		dest:    dest,
		done:    make(chan struct{}),
	}
}

func (t *transaction) ID() int32 {
	return t.request.GetTransactionID()
}

func (t *transaction) Done() {
	close(t.done)
}
