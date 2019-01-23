package peerwriter

import (
	"bytes"
	"container/list"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerprotocol"
)

const keepAlivePeriod = 2 * time.Minute

type PeerWriter struct {
	conn       net.Conn
	queueC     chan peerprotocol.Message
	cancelC    chan peerprotocol.CancelMessage
	writeQueue *list.List
	writeC     chan peerprotocol.Message
	messages   chan interface{}
	log        logger.Logger
	stopC      chan struct{}
	doneC      chan struct{}
}

func New(conn net.Conn, l logger.Logger) *PeerWriter {
	return &PeerWriter{
		conn:       conn,
		queueC:     make(chan peerprotocol.Message),
		cancelC:    make(chan peerprotocol.CancelMessage),
		writeQueue: list.New(),
		writeC:     make(chan peerprotocol.Message),
		messages:   make(chan interface{}),
		log:        l,
		stopC:      make(chan struct{}),
		doneC:      make(chan struct{}),
	}
}

func (p *PeerWriter) Messages() <-chan interface{} {
	return p.messages
}

func (p *PeerWriter) SendMessage(msg peerprotocol.Message) {
	select {
	case p.queueC <- msg:
	case <-p.doneC:
	}
}

func (p *PeerWriter) SendPiece(msg peerprotocol.RequestMessage, pi io.ReaderAt) {
	m := Piece{Piece: pi, Index: msg.Index, Begin: msg.Begin, Length: msg.Length}
	select {
	case p.queueC <- m:
	case <-p.doneC:
	}
}

func (p *PeerWriter) CancelRequest(msg peerprotocol.CancelMessage) {
	select {
	case p.cancelC <- msg:
	case <-p.doneC:
	}
}

func (p *PeerWriter) Stop() {
	close(p.stopC)
}

func (p *PeerWriter) Done() chan struct{} {
	return p.doneC
}

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
		case cm := <-p.cancelC:
			p.cancelRequest(cm)
		case <-p.stopC:
			return
		}
	}
}

func (p *PeerWriter) queueMessage(msg peerprotocol.Message) {
	if _, ok := msg.(peerprotocol.ChokeMessage); ok {
		p.cancelQueuedPieceMessages()
	}
	p.writeQueue.PushBack(msg)
}

func (p *PeerWriter) cancelQueuedPieceMessages() {
	var next *list.Element
	for e := p.writeQueue.Front(); e != nil; e = next {
		next = e.Next()
		if _, ok := e.Value.(Piece); ok {
			p.writeQueue.Remove(e)
		}
	}
}

func (p *PeerWriter) cancelRequest(cm peerprotocol.CancelMessage) {
	for e := p.writeQueue.Front(); e != nil; e = e.Next() {
		if pi, ok := e.Value.(Piece); ok && pi.Index == cm.Index && pi.Begin == cm.Begin && pi.Length == cm.Length {
			p.writeQueue.Remove(e)
			break
		}
	}
}

func (p *PeerWriter) messageWriter() {
	defer p.conn.Close()

	// Disable write deadline that is previously set by handshaker.
	err := p.conn.SetWriteDeadline(time.Time{})
	if err != nil {
		p.log.Error(err)
		return
	}

	keepAliveTicker := time.NewTicker(keepAlivePeriod / 2)
	defer keepAliveTicker.Stop()

	for {
		select {
		case msg := <-p.writeC:
			// p.log.Debugf("writing message of type: %q", msg.ID())
			payload, err := msg.MarshalBinary()
			if err != nil {
				p.log.Errorf("cannot marshal message [%v]: %s", msg.ID(), err.Error())
				return
			}
			buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
			var header = struct {
				Length uint32
				ID     peerprotocol.MessageID
			}{
				Length: uint32(1 + len(payload)),
				ID:     msg.ID(),
			}
			_ = binary.Write(buf, binary.BigEndian, &header)
			buf.Write(payload)
			n, err := p.conn.Write(buf.Bytes())
			p.countUploadBytes(msg, n)
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

func (p *PeerWriter) countUploadBytes(msg peerprotocol.Message, n int) {
	if _, ok := msg.(Piece); ok {
		uploaded := uint32(n) - 13
		if uploaded < 0 {
			uploaded = 0
		}
		if uploaded > 0 {
			select {
			case p.messages <- BlockUploaded{Length: uploaded}:
			case <-p.stopC:
			}
		}
	}
}
