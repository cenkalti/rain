package acceptor

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
)

type Acceptor struct {
	listener net.Listener
	newConns chan net.Conn
	closeC   chan struct{}
	doneC    chan struct{}
	log      logger.Logger
}

func New(lis net.Listener, newConns chan net.Conn, l logger.Logger) *Acceptor {
	return &Acceptor{
		listener: lis,
		newConns: newConns,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
		log:      l,
	}
}

func (a *Acceptor) Close() {
	close(a.closeC)
	<-a.doneC
}

func (a *Acceptor) Run() {
	defer close(a.doneC)

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-a.closeC:
			a.listener.Close()
		case <-done:
		}
	}()

	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.closeC:
			default:
				a.log.Error(err)
			}
			return
		}
		select {
		case a.newConns <- conn:
		case <-a.closeC:
			conn.Close()
			return
		}
	}
}
