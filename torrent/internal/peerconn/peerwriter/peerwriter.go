package peerwriter

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

const keepAlivePeriod = 2 * time.Minute

type PeerWriter struct {
	conn                  net.Conn
	queueC                chan peerprotocol.Message
	writeQueue            []peerprotocol.Message
	writeC                chan peerprotocol.Message
	uploadedBytesCounterC chan int64
	log                   logger.Logger
	stopC                 chan struct{}
	doneC                 chan struct{}
}

func New(conn net.Conn, l logger.Logger, uploadedBytesCounterC chan int64) *PeerWriter {
	return &PeerWriter{
		conn:                  conn,
		queueC:                make(chan peerprotocol.Message),
		writeQueue:            make([]peerprotocol.Message, 0),
		writeC:                make(chan peerprotocol.Message),
		uploadedBytesCounterC: uploadedBytesCounterC,
		log:                   l,
		stopC:                 make(chan struct{}),
		doneC:                 make(chan struct{}),
	}
}

func (p *PeerWriter) SendMessage(msg peerprotocol.Message) {
	select {
	case p.queueC <- msg:
	case <-p.doneC:
	}
}

func (p *PeerWriter) SendPiece(msg peerprotocol.RequestMessage, pi *pieceio.Piece) {
	m := Piece{Piece: pi, Begin: msg.Begin, Length: msg.Length}
	select {
	case p.queueC <- m:
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
		if len(p.writeQueue) == 0 {
			select {
			case msg := <-p.queueC:
				p.queueMessage(msg)
			case <-p.stopC:
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
		case <-p.stopC:
			return
		}
	}
}

func (p *PeerWriter) queueMessage(msg peerprotocol.Message) {
	// TODO cancel pending requests on choke
	p.writeQueue = append(p.writeQueue, msg)
}

func (p *PeerWriter) messageWriter() {
	keepAliveTicker := time.NewTicker(keepAlivePeriod / 2)
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
			n, err := p.conn.Write(buf.Bytes())
			p.countUploadBytes(msg, n)
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
		case <-p.stopC:
			return
		}
	}
}

func (p *PeerWriter) countUploadBytes(msg peerprotocol.Message, n int) {
	if _, ok := msg.(Piece); ok {
		uploaded := int64(n) - 13
		if uploaded < 0 {
			uploaded = 0
		}
		if uploaded > 0 {
			select {
			case p.uploadedBytesCounterC <- uploaded:
			case <-p.stopC:
			}
		}
	}
}
