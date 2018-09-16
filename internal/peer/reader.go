package peer

import (
	"encoding/binary"
	"io"
	"io/ioutil"
	"time"

	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

// MaxAllowedBlockSize is the max size of block data that we accept from peers.
const MaxAllowedBlockSize = 32 * 1024

const connReadTimeout = 3 * time.Minute

func (p *Peer) reader(stopC chan struct{}) {
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

		var id peerprotocol.MessageID
		err = binary.Read(p.conn, binary.BigEndian, &id)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type: %q", id)

		switch id {
		case peerprotocol.Choke:
			select {
			case p.messages.Choke <- p:
			case <-stopC:
				return
			}
		case peerprotocol.Unchoke:
			select {
			case p.messages.Unchoke <- p:
			case <-stopC:
				return
			}
		case peerprotocol.Interested:
			select {
			case p.messages.Interested <- p:
			case <-stopC:
				return
			}
		case peerprotocol.NotInterested:
			select {
			case p.messages.NotInterested <- p:
			case <-stopC:
				return
			}
		case peerprotocol.Have:
			var msg peerprotocol.HaveMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.Have <- Have{p, msg}:
			case <-stopC:
				return
			}
		case peerprotocol.Bitfield:
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
		case peerprotocol.Request:
			var msg peerprotocol.RequestMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Request: %+v", msg)

			if msg.Length > MaxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			select {
			case p.messages.Request <- Request{p, msg}:
			case <-stopC:
				return
			}
		case peerprotocol.Reject:
			if !p.FastExtension {
				p.log.Error("reject message received but fast extensions is not enabled")
				return
			}
			var msg peerprotocol.RequestMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Reject: %+v", msg)

			if msg.Length > MaxAllowedBlockSize {
				p.log.Error("received a reject with block size larger than allowed")
				return
			}
			select {
			case p.messages.Reject <- Request{p, msg}:
			case <-stopC:
				return
			}
		case peerprotocol.Piece:
			var msg peerprotocol.PieceMessage
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
		case peerprotocol.HaveAll:
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
		case peerprotocol.HaveNone:
			if !p.FastExtension {
				p.log.Error("have_none message received but fast extensions is not enabled")
				return
			}
			if !first {
				p.log.Error("have_none can only be sent after handshake")
				return
			}
		case peerprotocol.Suggest:
		case peerprotocol.AllowedFast:
			var msg peerprotocol.HaveMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			select {
			case p.messages.AllowedFast <- Have{p, msg}:
			case <-stopC:
				return
			}
		// case peerprotocol.Cancel: TODO handle cancel messages
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
