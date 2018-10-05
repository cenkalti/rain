package peerwriter

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
	"github.com/cenkalti/rain/internal/piece"
)

const keepAlivePeriod = 2 * time.Minute

type PeerWriter struct {
	conn       net.Conn
	queueC     chan peerprotocol.Message
	writeQueue []peerprotocol.Message
	writeC     chan peerprotocol.Message
	log        logger.Logger
}

func New(conn net.Conn, l logger.Logger) *PeerWriter {
	return &PeerWriter{
		conn:       conn,
		queueC:     make(chan peerprotocol.Message),
		writeQueue: make([]peerprotocol.Message, 0),
		writeC:     make(chan peerprotocol.Message),
		log:        l,
	}
}

func (p *PeerWriter) SendMessage(msg peerprotocol.Message, stopC chan struct{}) {
	select {
	case p.queueC <- msg:
	case <-stopC:
	}
}

func (p *PeerWriter) SendPiece(msg peerprotocol.RequestMessage, pi *piece.Piece, stopC chan struct{}) {
	m := Piece{Piece: pi, Begin: msg.Begin, Length: msg.Length}
	select {
	case p.queueC <- m:
	case <-stopC:
	}
}

func (p *PeerWriter) Run(stopC chan struct{}) {
	go p.messageWriter(stopC)
	for {
		if len(p.writeQueue) == 0 {
			select {
			case msg := <-p.queueC:
				p.queueMessage(msg)
			case <-stopC:
				return
			}
		}
		msg := p.writeQueue[0]
		select {
		case msg = <-p.queueC:
			p.queueMessage(msg)
		case p.writeC <- msg:
			// TODO peer write queue array grows indefinitely. Try using linked list.
			p.writeQueue = p.writeQueue[1:]
		case <-stopC:
			return
		}
	}
}

func (p *PeerWriter) queueMessage(msg peerprotocol.Message) {
	// TODO cancel pending requests on choke
	p.writeQueue = append(p.writeQueue, msg)
}

func (p *PeerWriter) messageWriter(stopC chan struct{}) {
	keepAliveTicker := time.NewTicker(keepAlivePeriod)
	defer keepAliveTicker.Stop()
	for {
		select {
		case msg := <-p.writeC:
			p.log.Debugf("writing message of type: %q", msg.ID())
			payload, err := msg.MarshalBinary()
			if err != nil {
				p.log.Errorf("cannot marshal message [%v]: %s", msg.ID(), err.Error())
				p.conn.Close()
				return
			}
			buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
			var header = struct {
				Length uint32
				ID     peerprotocol.MessageID
			}{
				uint32(1 + len(payload)),
				msg.ID(),
			}
			_ = binary.Write(buf, binary.BigEndian, &header)
			buf.Write(payload)
			_, err = p.conn.Write(buf.Bytes())
			if err != nil {
				p.log.Errorf("cannot write message [%v]: %s", msg.ID(), err.Error())
				p.conn.Close()
				return
			}
		case <-keepAliveTicker.C:
			_, err := p.conn.Write([]byte{0, 0, 0, 0})
			if err != nil {
				p.log.Errorf("cannot write keepalive message: %s", err.Error())
				p.conn.Close()
				return
			}
		case <-stopC:
			return
		}
	}
}
