package peerreader

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"time"

	"github.com/cenkalti/rain/internal/bufferpool"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/peerprotocol"
	"github.com/cenkalti/rain/internal/piece"
)

const (
	// maxBlockSize is the max size of block data that we accept from peers.
	maxBlockSize = 32 * 1024
	// time to wait for a message. peer must send keep-alive messages to keep connection alive.
	readTimeout = 2 * time.Minute
)

var blockPool = bufferpool.New(piece.BlockSize)

type PeerReader struct {
	conn         net.Conn
	buf          *bufio.Reader
	log          logger.Logger
	pieceTimeout time.Duration
	messages     chan interface{}
	stopC        chan struct{}
	doneC        chan struct{}
}

func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, bufferSize int) *PeerReader {
	return &PeerReader{
		conn:         conn,
		buf:          bufio.NewReaderSize(conn, bufferSize),
		log:          l,
		pieceTimeout: pieceTimeout,
		messages:     make(chan interface{}),
		stopC:        make(chan struct{}),
		doneC:        make(chan struct{}),
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
		err = p.conn.SetReadDeadline(time.Now().Add(readTimeout))
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

		// p.log.Debugf("Received message of type: %q", id)

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
			var bm peerprotocol.BitfieldMessage
			bm.Data = make([]byte, length)
			_, err = io.ReadFull(p.buf, bm.Data)
			if err != nil {
				return
			}
			msg = bm
		case peerprotocol.Request:
			var rm peerprotocol.RequestMessage
			err = binary.Read(p.buf, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			// p.log.Debugf("Received Request: %+v", rm)

			if rm.Length > maxBlockSize {
				err = fmt.Errorf("received a request with block size larger than allowed (%d > %d)", rm.Length, maxBlockSize)
				return
			}
			msg = rm
		case peerprotocol.Reject:
			var rm peerprotocol.RejectMessage
			err = binary.Read(p.buf, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			p.log.Debugf("Received Reject: %+v", rm)
			msg = rm
		case peerprotocol.Cancel:
			var cm peerprotocol.CancelMessage
			err = binary.Read(p.buf, binary.BigEndian, &cm)
			if err != nil {
				return
			}
			if cm.Length > maxBlockSize {
				err = errors.New("received a cancel with block size larger than allowed")
				return
			}
			msg = cm
		case peerprotocol.Piece:
			var pm peerprotocol.PieceMessage
			err = binary.Read(p.buf, binary.BigEndian, &pm)
			if err != nil {
				return
			}
			var m int
			buf := blockPool.Get(int(length - 8))
			for {
				err = p.conn.SetReadDeadline(time.Now().Add(p.pieceTimeout))
				if err != nil {
					return
				}
				n, rerr := io.ReadFull(p.buf, buf.Data[m:])
				if rerr != nil {
					if nerr, ok := rerr.(net.Error); ok && nerr.Timeout() {
						// Peer didn't send the full block in allowed time.
						if n == 0 {
							// Disconnect if no bytes received.
							return
						}
						// Some bytes received, peer appears to be slow, keep receiving the rest.
						m += n
						continue
					}
					return
				}
				// Received full block.
				break
			}
			msg = Piece{PieceMessage: pm, Buffer: buf}
		case peerprotocol.HaveAll:
			if !first {
				err = errors.New("have_all can only be sent after handshake")
				return
			}
			msg = peerprotocol.HaveAllMessage{}
		case peerprotocol.HaveNone:
			if !first {
				err = errors.New("have_none can only be sent after handshake")
				return
			}
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
			var em peerprotocol.ExtensionMessage
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
		// Only message types defined in BEP 3 are counted.
		if id < 9 {
			first = false
		}
		select {
		case p.messages <- msg:
		case <-p.stopC:
			return
		}
	}
}
