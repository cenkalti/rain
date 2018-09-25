package peerreader

import (
	"encoding/binary"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

// MaxAllowedBlockSize is the max size of block data that we accept from peers.
const MaxAllowedBlockSize = 32 * 1024

const connReadTimeout = 3 * time.Minute

type PeerReader struct {
	conn          net.Conn
	log           logger.Logger
	messages      chan interface{}
	fastExtension bool
}

func New(conn net.Conn, l logger.Logger, fastExtension bool) *PeerReader {
	return &PeerReader{
		conn:          conn,
		log:           l,
		messages:      make(chan interface{}),
		fastExtension: fastExtension,
	}
}

func (p *PeerReader) Messages() <-chan interface{} {
	return p.messages
}

func (p *PeerReader) Run(stopC chan struct{}) {
	defer close(p.messages)
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
			select {
			case <-stopC:
			default:
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

		// TODO consider defining a type for peer message
		var msg interface{}

		switch id {
		case peerprotocol.Choke:
			msg = peerprotocol.ChokeMessage{}
		case peerprotocol.Unchoke:
			msg = peerprotocol.UnchokeMessage{}
		case peerprotocol.Interested:
			msg = peerprotocol.InterestedMessage{}
		case peerprotocol.NotInterested:
			msg = peerprotocol.NotInterestedMessage{}
		case peerprotocol.Have:
			var hm peerprotocol.HaveMessage
			err = binary.Read(p.conn, binary.BigEndian, &hm)
			if err != nil {
				p.log.Error(err)
				return
			}
			msg = hm
		case peerprotocol.Bitfield:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}
			var bm peerprotocol.BitfieldMessage
			bm.Data = make([]byte, length)
			_, err = io.ReadFull(p.conn, bm.Data)
			if err != nil {
				p.log.Error(err)
				return
			}
			msg = bm
		case peerprotocol.Request:
			var rm peerprotocol.RequestMessage
			err = binary.Read(p.conn, binary.BigEndian, &rm)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Request: %+v", rm)

			if rm.Length > MaxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			msg = rm
		case peerprotocol.Reject:
			if !p.fastExtension {
				p.log.Error("reject message received but fast extensions is not enabled")
				return
			}
			var rm peerprotocol.RejectMessage
			err = binary.Read(p.conn, binary.BigEndian, &rm)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Received Reject: %+v", rm)

			if rm.Length > MaxAllowedBlockSize {
				p.log.Error("received a reject with block size larger than allowed")
				return
			}
			msg = rm
		case peerprotocol.Piece:
			// TODO send a reader as message to read directly onto the piece buffer
			buf := make([]byte, length)
			_, err = io.ReadFull(p.conn, buf)
			if err != nil {
				p.log.Error(err)
				return
			}
			pm := peerprotocol.PieceMessage{Length: length - 8}
			err = pm.UnmarshalBinary(buf)
			if err != nil {
				p.log.Error(err)
				return
			}
			msg = pm
		case peerprotocol.HaveAll:
			if !p.fastExtension {
				p.log.Error("have_all message received but fast extensions is not enabled")
				return
			}
			if !first {
				p.log.Error("have_all can only be sent after handshake")
				return
			}
			msg = peerprotocol.HaveAllMessage{}
		case peerprotocol.HaveNone:
			if !p.fastExtension {
				p.log.Error("have_none message received but fast extensions is not enabled")
				return
			}
			if !first {
				p.log.Error("have_none can only be sent after handshake")
				return
			}
			msg = peerprotocol.HaveNoneMessage{}
		// case peerprotocol.Suggest:
		case peerprotocol.AllowedFast:
			var am peerprotocol.AllowedFastMessage
			err = binary.Read(p.conn, binary.BigEndian, &am)
			if err != nil {
				p.log.Error(err)
				return
			}
			msg = am
		// case peerprotocol.Cancel: TODO handle cancel messages
		// TODO handle extension messages
		case peerprotocol.Extension:
			buf := make([]byte, length)
			_, err = io.ReadFull(p.conn, buf)
			if err != nil {
				p.log.Error(err)
				return
			}
			em := peerprotocol.NewExtensionMessage(length - 1)
			err = em.UnmarshalBinary(buf)
			if err != nil {
				p.log.Error(err)
				return
			}
			msg = em.Payload
		default:
			p.log.Debugf("unhandled message type: %s", id)
			p.log.Debugln("Discarding", length, "bytes...")
			_, err = io.CopyN(ioutil.Discard, p.conn, int64(length))
			if err != nil {
				p.log.Error(err)
				return
			}
			first = false
			continue
		}
		first = false
		if msg == nil {
			panic("msg unset")
		}
		select {
		case p.messages <- msg:
		case <-stopC:
			return
		}
	}
}
