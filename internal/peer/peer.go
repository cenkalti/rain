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
	"github.com/cenkalti/rain/internal/torrentdata"
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type Peer struct {
	conn          net.Conn
	id            [20]byte
	data          *torrentdata.Data
	messages      *Messages
	FastExtension bool
	log           logger.Logger
}

func New(conn net.Conn, id [20]byte, extensions *bitfield.Bitfield, d *torrentdata.Data, l logger.Logger, messages *Messages) *Peer {
	return &Peer{
		conn:          conn,
		id:            id,
		data:          d,
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
	if p.FastExtension && p.data.Bitfield().All() {
		err = p.sendHaveAll()
	} else if p.FastExtension && p.data.Bitfield().Count() == 0 {
		err = p.sendHaveNone()
	} else {
		err = p.sendBitfield(p.data.Bitfield())
	}
	if err != nil {
		p.log.Error(err)
		return
	}

	// TODO remove after implementing uploader
	p.SendUnchoke()
	p.SendInterested()

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
			var h haveMessage
			err = binary.Read(p.conn, binary.BigEndian, &h)
			if err != nil {
				p.log.Error(err)
				return
			}
			if h.Index >= uint32(len(p.data.Pieces)) {
				p.log.Error("unexpected piece index")
				return
			}
			pi := &p.data.Pieces[h.Index]
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", pi.Index)
			select {
			case p.messages.Have <- Have{p, pi}:
			case <-stopC:
				return
			}
		case messageid.Bitfield:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}
			numBytes := uint32(bitfield.NumBytes(uint32(len(p.data.Pieces))))
			if length != numBytes {
				p.log.Error("invalid bitfield length")
				return
			}
			b := make([]byte, numBytes)
			_, err = io.ReadFull(p.conn, b)
			if err != nil {
				p.log.Error(err)
				return
			}
			bf := bitfield.NewBytes(b, uint32(len(p.data.Pieces)))
			p.log.Debugln("Received bitfield:", bf.Hex())
			select {
			case p.messages.Bitfield <- Bitfield{p, bf}:
			case <-stopC:
				return
			}
		case messageid.Request:
			var req requestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Request: %+v", req)

			if req.Index >= uint32(len(p.data.Pieces)) {
				p.log.Error("invalid request: index")
				return
			}
			if req.Length > maxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > p.data.Pieces[req.Index].Length {
				p.log.Error("invalid request: length")
			}

			pi := &p.data.Pieces[req.Index]
			select {
			case p.messages.Request <- Request{p, pi, req.Begin, req.Length}:
			case <-stopC:
				return
			}
		case messageid.Piece:
			var msg pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			if msg.Index >= uint32(len(p.data.Pieces)) {
				p.log.Error("invalid piece index")
				return
			}
			piece := &p.data.Pieces[msg.Index]

			block := piece.GetBlock(msg.Begin)
			if block == nil {
				p.log.Error("invalid block begin")
				return
			}
			if length != block.Length {
				p.log.Error("invalid block length")
				return
			}

			pm := Piece{Peer: p, Piece: piece, Block: block}
			pm.Data = make([]byte, length)
			if _, err = io.ReadFull(p.conn, pm.Data); err != nil {
				p.log.Error(err)
				return
			}

			select {
			case p.messages.Piece <- pm:
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
		// case messageid.Reject: // TODO handle reject messages
		case messageid.AllowedFast:
			var h haveMessage
			err = binary.Read(p.conn, binary.BigEndian, &h)
			if err != nil {
				p.log.Error(err)
				return
			}
			if h.Index >= uint32(len(p.data.Pieces)) {
				p.log.Error("unexpected piece index")
				return
			}
			pi := &p.data.Pieces[h.Index]
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has allowed fast for piece #", pi.Index)
			select {
			case p.messages.AllowedFast <- Have{p, pi}:
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
	req := haveMessage{piece}
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
	req := requestMessage{piece, begin, length}
	p.log.Debugf("Sending Request: %+v", req)
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(messageid.Request, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {
	msg := pieceMessage{index, begin}
	p.log.Debugf("Sending Piece: %+v", msg)
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	_ = binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.writeMessage(messageid.Piece, buf.Bytes())
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
