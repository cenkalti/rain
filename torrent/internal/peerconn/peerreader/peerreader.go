package peerreader

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
)

const (
	// BlockSize is the max size of block data that we accept from peers.
	BlockSize = 16 * 1024
	// ReadTimeout is max allowed duration to read single messsage except Piece message.
	ReadTimeout = 2 * time.Minute // equal to keep-alive period
	// PieceTimeout is the duration that if no bytes received in PieceTimeout period, connection will be closed.
	PieceTimeout = 60 * time.Second
)

type PeerReader struct {
	conn              net.Conn
	buf               *bufio.Reader
	log               logger.Logger
	messages          chan interface{}
	fastExtension     bool
	extensionProtocol bool
	stopC             chan struct{}
	doneC             chan struct{}
}

func New(conn net.Conn, l logger.Logger, fastExtension, extensionProtocol bool) *PeerReader {
	return &PeerReader{
		conn:              conn,
		buf:               bufio.NewReaderSize(conn, 4+1+8+BlockSize),
		log:               l,
		messages:          make(chan interface{}),
		fastExtension:     fastExtension,
		extensionProtocol: extensionProtocol,
		stopC:             make(chan struct{}),
		doneC:             make(chan struct{}),
	}
}

func (p *PeerReader) Messages() <-chan interface{} {
	return p.messages
}

func (p *PeerReader) Stop() {
	close(p.stopC)
}

func (p *PeerReader) Done() chan struct{} {
	return p.doneC
}

func (p *PeerReader) Run() {
	defer close(p.doneC)
	defer close(p.messages)

	var err error
	defer func() {
		if err == nil {
			return
		} else if err == io.EOF { // peer closed the connection
			return
		} else if err == io.ErrUnexpectedEOF {
			return
		} else if _, ok := err.(*net.OpError); ok {
			return
		}
		select {
		case <-p.stopC: // don't log error if peer is stopped
		default:
			p.log.Error(err)
		}
	}()

	first := true
	for {
		err = p.conn.SetReadDeadline(time.Now().Add(ReadTimeout))
		if err != nil {
			return
		}

		var length uint32
		// p.log.Debug("Reading message...")
		err = binary.Read(p.buf, binary.BigEndian, &length)
		if err != nil {
			return
		}
		// p.log.Debugf("Received message of length: %d", length)

		if length == 0 { // keep-alive message
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}

		var id peerprotocol.MessageID
		err = binary.Read(p.buf, binary.BigEndian, &id)
		if err != nil {
			return
		}
		length--

		p.log.Debugf("Received message of type: %q", id)

		// TODO consider defining a type for peer message
		var msg interface{}

		switch id {
		case peerprotocol.Choke:
			first = false
			msg = peerprotocol.ChokeMessage{}
		case peerprotocol.Unchoke:
			first = false
			msg = peerprotocol.UnchokeMessage{}
		case peerprotocol.Interested:
			first = false
			msg = peerprotocol.InterestedMessage{}
		case peerprotocol.NotInterested:
			first = false
			msg = peerprotocol.NotInterestedMessage{}
		case peerprotocol.Have:
			first = false
			var hm peerprotocol.HaveMessage
			err = binary.Read(p.buf, binary.BigEndian, &hm)
			if err != nil {
				return
			}
			msg = hm
		case peerprotocol.Bitfield:
			if !first {
				err = errors.New("bitfield can only be sent after handshake")
				return
			}
			first = false
			var bm peerprotocol.BitfieldMessage
			bm.Data = make([]byte, length)
			_, err = io.ReadFull(p.buf, bm.Data)
			if err != nil {
				return
			}
			msg = bm
		case peerprotocol.Request:
			first = false
			var rm peerprotocol.RequestMessage
			err = binary.Read(p.buf, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			p.log.Debugf("Received Request: %+v", rm)

			if rm.Length > BlockSize {
				err = errors.New("received a request with block size larger than allowed")
				return
			}
			msg = rm
		case peerprotocol.Reject:
			if !p.fastExtension {
				err = errors.New("reject message received but fast extensions is not enabled")
				return
			}
			var rm peerprotocol.RejectMessage
			err = binary.Read(p.buf, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			p.log.Debugf("Received Reject: %+v", rm)
			msg = rm
		// TODO handle cancel message
		case peerprotocol.Piece:
			first = false
			var pm peerprotocol.PieceMessage
			err = binary.Read(p.buf, binary.BigEndian, &pm)
			if err != nil {
				return
			}
			var m, n int
			b := make([]byte, length-8)
			for {
				err = p.conn.SetReadDeadline(time.Now().Add(PieceTimeout))
				if err != nil {
					return
				}
				n, err = io.ReadFull(p.buf, b)
				if err != nil {
					if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
						// Peer couldn't send the block in allowed time.
						if n == 0 {
							return
						}
						m += n
						b = b[m:]
						continue
					}
					return
				}
				break
			}
			msg = Piece{PieceMessage: pm, Data: b}
		case peerprotocol.HaveAll:
			if !p.fastExtension {
				err = errors.New("have_all message received but fast extensions is not enabled")
				return
			}
			if !first {
				err = errors.New("have_all can only be sent after handshake")
				return
			}
			first = false
			msg = peerprotocol.HaveAllMessage{}
		case peerprotocol.HaveNone:
			if !p.fastExtension {
				err = errors.New("have_none message received but fast extensions is not enabled")
				return
			}
			if !first {
				err = errors.New("have_none can only be sent after handshake")
				return
			}
			first = false
			msg = peerprotocol.HaveNoneMessage{}
		case peerprotocol.AllowedFast:
			var am peerprotocol.AllowedFastMessage
			err = binary.Read(p.buf, binary.BigEndian, &am)
			if err != nil {
				return
			}
			msg = am
		case peerprotocol.Extension:
			buf := make([]byte, length)
			_, err = io.ReadFull(p.buf, buf)
			if err != nil {
				return
			}
			if !p.extensionProtocol {
				err = errors.New("extension message received but it is not enabled in bitfield")
				break
			}
			em := peerprotocol.NewExtensionMessage(length - 1)
			err = em.UnmarshalBinary(buf)
			if err != nil {
				return
			}
			msg = em.Payload
		default:
			p.log.Debugf("unhandled message type: %s", id)
			p.log.Debugln("Discarding", length, "bytes...")
			_, err = io.CopyN(ioutil.Discard, p.buf, int64(length))
			if err != nil {
				return
			}
			continue
		}
		if msg == nil {
			panic("msg unset")
		}
		select {
		case p.messages <- msg:
		case <-p.stopC:
			return
		}
	}
}
