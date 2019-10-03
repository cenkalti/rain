package acceptor

import (
	"net"

	"github.com/cenkalti/rain/internal/logger"
)

// Acceptor accepts sockets from a listener and sends to a channel.
type Acceptor struct {
	listener net.Listener
	newConns chan net.Conn
	closeC   chan struct{}
	doneC    chan struct{}
	log      logger.Logger
}

// New returns a new Acceptor.
func New(lis net.Listener, newConns chan net.Conn, l logger.Logger) *Acceptor {
	return &Acceptor{
		listener: lis,
		newConns: newConns,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
		log:      l,
	}
}

// Close the acceptor and the listener.
func (a *Acceptor) Close() {
	close(a.closeC)
	<-a.doneC
}

// Run the acceptor.
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
