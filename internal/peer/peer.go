package peer

import (
	"net"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/peer/peerwriter"
	"github.com/cenkalti/rain/internal/piece"
)

type Peer struct {
	conn          net.Conn
	id            [20]byte
	FastExtension bool
	messages      *Messages
	writer        *peerwriter.PeerWriter
	log           logger.Logger
}

func New(conn net.Conn, id [20]byte, extensions *bitfield.Bitfield, l logger.Logger, messages *Messages) *Peer {
	return &Peer{
		conn:          conn,
		id:            id,
		FastExtension: extensions.Test(61),
		messages:      messages,
		writer:        peerwriter.New(conn, l),
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

func (p *Peer) SendMessage(msg peerprotocol.Message, stopC chan struct{}) {
	p.writer.SendMessage(msg, stopC)
}

func (p *Peer) SendPiece(msg peerprotocol.RequestMessage, pi *piece.Piece, stopC chan struct{}) {
	p.writer.SendPiece(msg, pi, stopC)
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

	readerDone := make(chan struct{})
	go func() {
		p.reader(stopC)
		close(readerDone)
	}()

	writerDone := make(chan struct{})
	go func() {
		p.writer.Run(stopC)
		close(writerDone)
	}()

	select {
	case <-stopC:
		p.conn.Close()
		<-readerDone
		<-writerDone
	case <-readerDone:
		p.conn.Close()
		<-writerDone
	case <-writerDone:
		p.conn.Close()
		<-readerDone
	}

	select {
	case p.messages.Disconnect <- p:
	case <-stopC:
	}
}
