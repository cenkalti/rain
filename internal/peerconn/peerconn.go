package peerconn

import (
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerconn/peerwriter"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/juju/ratelimit"
)

// Conn is a peer connection that provides a channel for receiving messages and methods for sending messages.
type Conn struct {
	conn     net.Conn
	reader   *peerreader.PeerReader
	writer   *peerwriter.PeerWriter
	messages chan interface{}
	log      logger.Logger
	closeC   chan struct{}
	doneC    chan struct{}
}

// New returns a new PeerConn by wrapping a net.Conn.
func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, maxRequestsIn int, fastEnabled bool, br, bw *ratelimit.Bucket) *Conn {
	return &Conn{
		conn:     conn,
		reader:   peerreader.New(conn, l, pieceTimeout, br),
		writer:   peerwriter.New(conn, l, maxRequestsIn, fastEnabled, bw),
		messages: make(chan interface{}),
		log:      l,
		closeC:   make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

// Addr returns the net.TCPAddr of the peer.
func (p *Conn) Addr() *net.TCPAddr {
	return p.conn.RemoteAddr().(*net.TCPAddr)
}

// IP returns the string representation of IP address.
func (p *Conn) IP() string {
	return p.conn.RemoteAddr().(*net.TCPAddr).IP.String()
}

// String returns the remote address as string.
func (p *Conn) String() string {
	return p.conn.RemoteAddr().String()
}

// Close stops receiving and sending messages and closes underlying net.Conn.
func (p *Conn) Close() {
	close(p.closeC)
	<-p.doneC
}

// Logger for the peer that logs messages prefixed with peer address.
func (p *Conn) Logger() logger.Logger {
	return p.log
}

// Messages received from the peer will be sent to the channel returned.
// The channel and underlying net.Conn will be closed if any error occurs while receiving or sending.
func (p *Conn) Messages() <-chan interface{} {
	return p.messages
}

// SendMessage queues a message for sending. Does not block.
func (p *Conn) SendMessage(msg peerprotocol.Message) {
	p.writer.SendMessage(msg)
}

// SendPiece queues a piece message for sending. Does not block.
// Piece data is read just before the message is sent.
// If queued messages greater than `maxRequestsIn` specified in constructor, the last message is dropped.
func (p *Conn) SendPiece(msg peerprotocol.RequestMessage, pi io.ReaderAt) {
	p.writer.SendPiece(msg, pi)
}

// CancelRequest removes previously queued piece message matching msg.
func (p *Conn) CancelRequest(msg peerprotocol.CancelMessage) {
	p.writer.CancelRequest(msg)
}

// Run starts receiving messages from peer and starts sending queued messages.
// If any error happens during receiving or sending messages,
// the connection and the underlying net.Conn will be closed.
func (p *Conn) Run() {
	defer close(p.doneC)
	defer close(p.messages)

	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	go p.reader.Run()
	defer func() { <-p.reader.Done() }()

	go p.writer.Run()
	defer func() { <-p.writer.Done() }()

	defer p.conn.Close()
	for {
		select {
		case msg := <-p.reader.Messages():
			select {
			case p.messages <- msg:
			case <-p.closeC:
			}
		case msg := <-p.writer.Messages():
			select {
			case p.messages <- msg:
			case <-p.closeC:
			}
		case <-p.closeC:
			p.reader.Stop()
			p.writer.Stop()
			return
		case <-p.reader.Done():
			p.writer.Stop()
			return
		case <-p.writer.Done():
			p.reader.Stop()
			return
		}
	}
}
