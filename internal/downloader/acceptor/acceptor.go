package acceptor

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
)

type Acceptor struct {
	listener net.Listener
	newConns chan net.Conn
	log      logger.Logger
}

func New(lis net.Listener, newConns chan net.Conn, l logger.Logger) *Acceptor {
	return &Acceptor{
		listener: lis,
		newConns: newConns,
		log:      l,
	}
}

func (a *Acceptor) Run(stopC chan struct{}) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-stopC:
			a.listener.Close()
		case <-done:
		}
	}()

	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-stopC:
			default:
				a.log.Error(err)
			}
			return
		}
		select {
		case a.newConns <- conn:
		case <-stopC:
			conn.Close()
			return
		}
	}
}
