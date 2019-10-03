package peerwriter

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/juju/ratelimit"
)

const keepAlivePeriod = 2 * time.Minute

// PeerWriter is responsible for writing BitTorrent protocol messages to the peer connection.
type PeerWriter struct {
	conn                  net.Conn
	queueC                chan peerprotocol.Message
	cancelC               chan peerprotocol.CancelMessage
	writeQueue            *list.List
	maxQueuedRequests     int
	fastEnabled           bool
	currentQueuedRequests int
	writeC                chan peerprotocol.Message
	messages              chan interface{}
	servedRequests        map[peerprotocol.RequestMessage]struct{}
	bucket                *ratelimit.Bucket
	log                   logger.Logger
	stopC                 chan struct{}
	doneC                 chan struct{}
}

// New returns a new PeerWriter by wrapping a net.Conn.
func New(conn net.Conn, l logger.Logger, maxQueuedRequests int, fastEnabled bool, b *ratelimit.Bucket) *PeerWriter {
	return &PeerWriter{
		conn:              conn,
		queueC:            make(chan peerprotocol.Message),
		cancelC:           make(chan peerprotocol.CancelMessage),
		writeQueue:        list.New(),
		maxQueuedRequests: maxQueuedRequests,
		fastEnabled:       fastEnabled,
		writeC:            make(chan peerprotocol.Message),
		messages:          make(chan interface{}),
		servedRequests:    make(map[peerprotocol.RequestMessage]struct{}),
		bucket:            b,
		log:               l,
		stopC:             make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

// Messages returns a channel. Various events from the writer are sent to this channel.
func (p *PeerWriter) Messages() <-chan interface{} {
	return p.messages
}

// SendMessage is used to send a protocol message to the Peer.
func (p *PeerWriter) SendMessage(msg peerprotocol.Message) {
	select {
	case p.queueC <- msg:
	case <-p.doneC:
	}
}

// SendPiece is used to send a "piece" message to the Peer.
// Data is not read when the method is called.
// Data is read by the run loop when writing the piece message.
func (p *PeerWriter) SendPiece(msg peerprotocol.RequestMessage, pi io.ReaderAt) {
	m := Piece{Data: pi, RequestMessage: msg}
	select {
	case p.queueC <- m:
	case <-p.doneC:
	}
}

// CancelRequest cancels the previously received "request" message.
func (p *PeerWriter) CancelRequest(msg peerprotocol.CancelMessage) {
	select {
	case p.cancelC <- msg:
	case <-p.doneC:
	}
}

// Stop the writer loop.
func (p *PeerWriter) Stop() {
	close(p.stopC)
}

// Done returns a channel that is closed when run loop exists.
func (p *PeerWriter) Done() chan struct{} {
	return p.doneC
}

// Run the writer loop.
func (p *PeerWriter) Run() {
	defer close(p.doneC)

	go p.messageWriter()

	for {
		var (
			e      *list.Element
			msg    peerprotocol.Message
			writeC chan peerprotocol.Message
		)
		if p.writeQueue.Len() > 0 {
			e = p.writeQueue.Front()
			msg = e.Value.(peerprotocol.Message)
			writeC = p.writeC
		}
		select {
		case msg = <-p.queueC:
			p.queueMessage(msg)
		case writeC <- msg:
			p.writeQueue.Remove(e)
			if _, ok := msg.(Piece); ok {
				p.currentQueuedRequests--
			}
		case cm := <-p.cancelC:
			p.cancelRequest(cm)
		case <-p.stopC:
			return
		}
	}
}

func (p *PeerWriter) queueMessage(msg peerprotocol.Message) {
	switch msg2 := msg.(type) {
	case peerprotocol.ChokeMessage:
		p.cancelQueuedPieceMessages()
	case Piece:
		// Reject request if peer queued to many requests
		if p.currentQueuedRequests >= p.maxQueuedRequests {
			if p.fastEnabled {
				msg = peerprotocol.RejectMessage{RequestMessage: msg2.RequestMessage}
				break
			} else {
				// Drop message silently
				return
			}
		}
		p.currentQueuedRequests++
	}
	p.writeQueue.PushBack(msg)
}

func (p *PeerWriter) cancelQueuedPieceMessages() {
	var next *list.Element
	for e := p.writeQueue.Front(); e != nil; e = next {
		next = e.Next()
		if _, ok := e.Value.(Piece); ok {
			p.writeQueue.Remove(e)
			p.currentQueuedRequests--
		}
	}
}

func (p *PeerWriter) cancelRequest(cm peerprotocol.CancelMessage) {
	for e := p.writeQueue.Front(); e != nil; e = e.Next() {
		if pi, ok := e.Value.(Piece); ok && pi.Index == cm.Index && pi.Begin == cm.Begin && pi.Length == cm.Length {
			p.writeQueue.Remove(e)
			p.currentQueuedRequests--
			break
		}
	}
}

func (p *PeerWriter) messageWriter() {
	defer p.conn.Close()

	// Disable write deadline that is previously set by handshaker.
	err := p.conn.SetWriteDeadline(time.Time{})
	if _, ok := err.(*net.OpError); ok {
		p.log.Debugln("cannot set deadline:", err)
		return
	}
	if err != nil {
		p.log.Error(err)
		return
	}

	keepAliveTicker := time.NewTicker(keepAlivePeriod / 2)
	defer keepAliveTicker.Stop()

	// Use a fixed-size array for slice storage.
	// Length is calculated for a piece message at max block size.
	// Length = 4 bytes length + 1 byte messageID + 8 bytes piece header + <MaxBlockSize> piece data
	// This will reduce allocations in loop below.
	var a [4 + 1 + 8 + peerreader.MaxBlockSize]byte
	b := a[:0]

	for {
		select {
		case msg := <-p.writeC:
			// Reject duplicate requests
			if pi, ok := msg.(Piece); ok {
				if _, ok = p.servedRequests[pi.RequestMessage]; ok {
					msg = peerprotocol.RejectMessage{RequestMessage: pi.RequestMessage}
				} else {
					p.servedRequests[pi.RequestMessage] = struct{}{}
				}
			}

			// p.log.Debugf("writing message of type: %q", msg.ID())

			buf := bytes.NewBuffer(b)

			// Reserve space for length and message ID
			buf.Write([]byte{0, 0, 0, 0, 0})

			var m int64
			if wt, ok := msg.(io.WriterTo); ok {
				m, err = wt.WriteTo(buf)
			} else {
				m, err = buf.ReadFrom(msg)
			}
			if err != nil {
				select {
				case <-p.stopC:
					return
				default:
				}
				p.log.Errorf("cannot serialize message [%v]: %s", msg.ID(), err.Error())
				return
			}

			// Put length
			binary.BigEndian.PutUint32(buf.Bytes()[:4], uint32(1+m))
			// Put message ID
			buf.Bytes()[4] = uint8(msg.ID())

			if _, ok := msg.(Piece); ok && p.bucket != nil {
				d := p.bucket.Take(int64(buf.Len()))
				select {
				case <-time.After(d):
				case <-p.stopC:
					return
				}
			}

			n, err := p.conn.Write(buf.Bytes())
			if _, ok := msg.(Piece); ok {
				p.countUploadBytes(n)
			}
			if _, ok := err.(*net.OpError); ok {
				p.log.Debugf("cannot write message [%v]: %s", msg.ID(), err.Error())
				return
			}
			if err != nil {
				p.log.Errorf("cannot write message [%v]: %s", msg.ID(), err.Error())
				return
			}
		case <-keepAliveTicker.C:
			_, err := p.conn.Write([]byte{0, 0, 0, 0})
			if _, ok := err.(*net.OpError); ok {
				p.log.Debugf("cannot write keepalive message: %s", err.Error())
				return
			}
			if err != nil {
				p.log.Errorf("cannot write keepalive message: %s", err.Error())
				return
			}
		case <-p.stopC:
			return
		}
	}
}

func (p *PeerWriter) countUploadBytes(n int) {
	n -= 13 // message + piece header
	if n < 0 {
		n = 0
	}
	uploaded := uint32(n)
	if uploaded > 0 {
		select {
		case p.messages <- BlockUploaded{Length: uploaded}:
		case <-p.stopC:
		}
	}
}
