package peer

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/messageid"
)

const connReadTimeout = 3 * time.Minute

// MaxAllowedBlockSize is the max size of block data that we accept from peers.
const MaxAllowedBlockSize = 32 * 1024

type Peer struct {
	conn          net.Conn
	id            [20]byte
	bitfield      *bitfield.Bitfield
	messages      *Messages
	FastExtension bool
	log           logger.Logger
}

func New(conn net.Conn, id [20]byte, extensions *bitfield.Bitfield, bf *bitfield.Bitfield, l logger.Logger, messages *Messages) *Peer {
	return &Peer{
		conn:          conn,
		id:            id,
		bitfield:      bf,
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
// TODO send keep-alive messages to peers at interval.
func (p *Peer) Run(stopC chan struct{}) {
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())
	defer func() {
		_ = p.conn.Close()
		select {
		case p.messages.Disconnect <- p:
		case <-stopC:
		}
	}()

	var err error
	if p.FastExtension && p.bitfield.All() {
		err = p.sendHaveAll()
	} else if p.FastExtension && p.bitfield.Count() == 0 {
		err = p.sendHaveNone()
	} else {
		err = p.sendBitfield(p.bitfield)
	}
	if err != nil {
		p.log.Error(err)
		return
	}

	select {
	case p.messages.Connect <- p:
	case <-stopC:
		return
	}

	first := true
	for {
		err := p.conn.SetReadDeadline(time.Now().Add(connReadTimeout))
		if err != nil {
			p.log.Error(err)
			return
		}

		var length uint32
		// p.log.Debug("Reading message...")
		err = binary.Read(p.conn, binary.BigEndian, &length)
		if err != nil {
			if err == io.EOF {
				p.log.Debug("Remote peer has closed the connection")
			} else {
				p.log.Error(err)
			}
			return
		}
		// p.log.Debugf("Received message of length: %d", length)

		if length == 0 { // keep-alive message
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}

		var id messageid.MessageID
		err = binary.Read(p.conn, binary.BigEndian, &id)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type: %q", id)

		switch id {
		case messageid.Choke:
			select {
			case p.messages.Choke <- p:
			case <-stopC:
				return
			}
		case messageid.Unchoke:
			select {
			case p.messages.Unchoke <- p:
			case <-stopC:
				return
			}
			// TODO implement
		case messageid.Interested:
			// TODO implement
		case messageid.NotInterested:
			// TODO implement
		case messageid.Have:
			var h HaveMessage
			err = binary.Read(p.conn, binary.BigEndian, &h)
			if err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.Have <- Have{p, h}:
			case <-stopC:
				return
			}
		case messageid.Bitfield:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}
			b := make([]byte, length)
			_, err = io.ReadFull(p.conn, b)
			if err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.Bitfield <- Bitfield{p, b}:
			case <-stopC:
				return
			}
		case messageid.Request:
			var req RequestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Request: %+v", req)

			if req.Length > MaxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			select {
			case p.messages.Request <- Request{p, req}:
			case <-stopC:
				return
			}
		case messageid.Reject:
			if !p.FastExtension {
				p.log.Error("reject message received but fast extensions is not enabled")
				return
			}
			var req RequestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Reject: %+v", req)

			if req.Length > MaxAllowedBlockSize {
				p.log.Error("received a reject with block size larger than allowed")
				return
			}
			select {
			case p.messages.Reject <- Request{p, req}:
			case <-stopC:
				return
			}
		case messageid.Piece:
			var msg PieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			data := make([]byte, length)
			if _, err = io.ReadFull(p.conn, data); err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.Piece <- Piece{p, msg, data}:
			case <-stopC:
				return
			}
		case messageid.HaveAll:
			if !p.FastExtension {
				p.log.Error("have_all message received but fast extensions is not enabled")
				return
			}
			if !first {
				p.log.Error("have_all can only be sent after handshake")
				return
			}
			select {
			case p.messages.HaveAll <- p:
			case <-stopC:
				return
			}
		case messageid.HaveNone:
			if !p.FastExtension {
				p.log.Error("have_none message received but fast extensions is not enabled")
				return
			}
			if !first {
				p.log.Error("have_none can only be sent after handshake")
				return
			}
		case messageid.Suggest:
		case messageid.AllowedFast:
			var h HaveMessage
			err = binary.Read(p.conn, binary.BigEndian, &h)
			if err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.AllowedFast <- Have{p, h}:
			case <-stopC:
				return
			}
		// case messageid.Cancel: TODO handle cancel messages
		default:
			p.log.Debugf("unhandled message type: %s", id)
			p.log.Debugln("Discarding", length, "bytes...")
			_, err = io.CopyN(ioutil.Discard, p.conn, int64(length))
			if err != nil {
				p.log.Error(err)
				return
			}
		}
		first = false
	}
}

func (p *Peer) sendBitfield(b *bitfield.Bitfield) error {
	// Sending bitfield may be omitted if have no pieces.
	if b.Count() == 0 {
		return nil
	}
	return p.writeMessage(messageid.Bitfield, b.Bytes())
}

func (p *Peer) SendInterested() error {
	return p.writeMessage(messageid.Interested, nil)
}

func (p *Peer) SendNotInterested() error {
	return p.writeMessage(messageid.NotInterested, nil)
}

func (p *Peer) SendChoke() error {
	return p.writeMessage(messageid.Choke, nil)
}

func (p *Peer) SendUnchoke() error {
	return p.writeMessage(messageid.Unchoke, nil)
}

func (p *Peer) SendHave(piece uint32) error {
	req := HaveMessage{piece}
	buf := bytes.NewBuffer(make([]byte, 0, 4))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(messageid.Have, buf.Bytes())
}

func (p *Peer) sendHaveAll() error {
	return p.writeMessage(messageid.HaveAll, nil)
}

func (p *Peer) sendHaveNone() error {
	return p.writeMessage(messageid.HaveNone, nil)
}

func (p *Peer) SendRequest(piece, begin, length uint32) error {
	req := RequestMessage{piece, begin, length}
	p.log.Debugf("Sending Request: %+v", req)
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(messageid.Request, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {
	msg := PieceMessage{index, begin}
	p.log.Debugf("Sending Piece: %+v", msg)
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	_ = binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.writeMessage(messageid.Piece, buf.Bytes())
}

func (p *Peer) SendReject(piece, begin, length uint32) error {
	req := RequestMessage{piece, begin, length}
	p.log.Debugf("Sending Reject: %+v", req)
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(messageid.Request, buf.Bytes())
}

func (p *Peer) writeMessage(id messageid.MessageID, payload []byte) error {
	p.log.Debugf("Sending message of type: %q", id)
	buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
	var header = struct {
		Length uint32
		ID     messageid.MessageID
	}{
		uint32(1 + len(payload)),
		id,
	}
	_ = binary.Write(buf, binary.BigEndian, &header)
	buf.Write(payload)
	_, err := p.conn.Write(buf.Bytes())
	return err
}
