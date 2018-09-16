package peer

import (
	"net"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
)

type Peer struct {
	conn          net.Conn
	id            [20]byte
	messages      *Messages
	FastExtension bool
	log           logger.Logger
}

func New(conn net.Conn, id [20]byte, extensions *bitfield.Bitfield, l logger.Logger, messages *Messages) *Peer {
	return &Peer{
		conn:          conn,
		id:            id,
		messages:      messages,
		FastExtension: extensions.Test(61),
		log:           l,
	}
}

func (p *Peer) ID() [20]byte {
	return p.id
}

func (p *Peer) String() string {
	return p.conn.RemoteAddr().String()
}

func (p *Peer) Close() {
	_ = p.conn.Close()
}

func (p *Peer) Logger() logger.Logger {
	return p.log
}

// Run reads and processes incoming messages after handshake.
// TODO send keep-alive messages to peers every 2 minutes.
func (p *Peer) Run(stopC chan struct{}) {
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	select {
	case p.messages.Connect <- p:
	case <-stopC:
		return
	}

	defer func() {
		select {
		case p.messages.Disconnect <- p:
		case <-stopC:
		}
	}()

	readerDone := make(chan struct{})
	go func() {
		p.reader(stopC)
		close(readerDone)
	}()

	go p.writer(stopC)

	select {
	case <-stopC:
		p.conn.Close()
	case <-readerDone:
	}
}
