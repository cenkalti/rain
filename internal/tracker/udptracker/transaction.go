package udptracker

import (
	"math/rand"
)

type transaction struct {
	request  udpReqeust
	response []byte
	err      error
	done     chan struct{}
}

func newTransaction(req udpReqeust) *transaction {
	req.SetTransactionID(rand.Int31())
	return &transaction{
		request: req,
		done:    make(chan struct{}),
	}
}

func (t *transaction) ID() int32 {
	return t.request.GetTransactionID()
}

func (t *transaction) Done() {
	close(t.done)
}
