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
	"github.com/juju/ratelimit"
)

const (
	// MaxBlockSize allowed in "request" messages.
	MaxBlockSize = 16 * 1024
	// time to wait for a message. peer must send keep-alive messages to keep connection alive.
	readTimeout = 2 * time.Minute
	// length + msgid + requestmsg
	readBufferSize = 4 + 1 + 12
)

var blockPool = bufferpool.New(piece.BlockSize)

// PeerReader is used for reading and parsing messages from a net.Conn.
type PeerReader struct {
	conn         net.Conn
	r            io.Reader
	log          logger.Logger
	pieceTimeout time.Duration
	bucket       *ratelimit.Bucket
	messages     chan interface{}
	stopC        chan struct{}
	doneC        chan struct{}
}

// New returns a new PeerReader by wrapping a net.Conn.
func New(conn net.Conn, l logger.Logger, pieceTimeout time.Duration, b *ratelimit.Bucket) *PeerReader {
	return &PeerReader{
		conn:         conn,
		r:            bufio.NewReaderSize(conn, readBufferSize),
		log:          l,
		pieceTimeout: pieceTimeout,
		bucket:       b,
		messages:     make(chan interface{}),
		stopC:        make(chan struct{}),
		doneC:        make(chan struct{}),
	}
}

// Messages returns a channel. All messages read by this PeerReader is sent to this channel.
func (p *PeerReader) Messages() <-chan interface{} {
	return p.messages
}

// Stop the read loop.
func (p *PeerReader) Stop() {
	close(p.stopC)
}

// Done returns a channel that is closed when the read loop exists.
func (p *PeerReader) Done() chan struct{} {
	return p.doneC
}

// Run the read loop.
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
		} else if err == errStoppedWhileWaitingBucket {
			return
		} else if _, ok := err.(*net.OpError); ok {
			return
		}
		select {
		case <-p.stopC: // don't log error if peer is stopped
		default:
			if _, ok := err.(*blockSizeError); ok {
				p.log.Debug(err)
			} else {
				p.log.Error(err)
			}
		}
	}()

	for {
		err = p.conn.SetReadDeadline(time.Now().Add(readTimeout))
		if err != nil {
			return
		}

		var length uint32
		// p.log.Debug("Reading message...")
		err = binary.Read(p.r, binary.BigEndian, &length)
		if err != nil {
			return
		}
		// p.log.Debugf("Received message of length: %d", length)

		if length == 0 { // keep-alive message
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}

		var id peerprotocol.MessageID
		err = binary.Read(p.r, binary.BigEndian, &id)
		if err != nil {
			return
		}
		length--

		// p.log.Debugf("Received message of type: %q", id)

		var msg interface{}

		switch id {
		case peerprotocol.Choke:
			p.log.Debug("Received Choke")
			msg = peerprotocol.ChokeMessage{}
		case peerprotocol.Unchoke:
			p.log.Debug("Received Unchoke")
			msg = peerprotocol.UnchokeMessage{}
		case peerprotocol.Interested:
			p.log.Debug("Received Interested")
			msg = peerprotocol.InterestedMessage{}
		case peerprotocol.NotInterested:
			p.log.Debug("Received NotInterested")
			msg = peerprotocol.NotInterestedMessage{}
		case peerprotocol.Have:
			var hm peerprotocol.HaveMessage
			err = binary.Read(p.r, binary.BigEndian, &hm)
			if err != nil {
				return
			}
			msg = hm
		case peerprotocol.Bitfield:
			var bm peerprotocol.BitfieldMessage
			bm.Data = make([]byte, length)
			_, err = io.ReadFull(p.r, bm.Data)
			if err != nil {
				return
			}
			msg = bm
		case peerprotocol.Request:
			var rm peerprotocol.RequestMessage
			err = binary.Read(p.r, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			// p.log.Debugf("Received Request: %+v", rm)

			if rm.Length > MaxBlockSize {
				err = &blockSizeError{
					messageID:  id,
					got:        rm.Length,
					allowedMax: MaxBlockSize,
				}
				return
			}
			msg = rm
		case peerprotocol.Reject:
			var rm peerprotocol.RejectMessage
			err = binary.Read(p.r, binary.BigEndian, &rm)
			if err != nil {
				return
			}
			p.log.Debugf("Received Reject: %+v", rm)
			msg = rm
		case peerprotocol.Cancel:
			var cm peerprotocol.CancelMessage
			err = binary.Read(p.r, binary.BigEndian, &cm)
			if err != nil {
				return
			}
			msg = cm
		case peerprotocol.Piece:
			var pm peerprotocol.PieceMessage
			err = binary.Read(p.r, binary.BigEndian, &pm)
			if err != nil {
				return
			}
			length -= 8
			if length > piece.BlockSize {
				err = &blockSizeError{
					messageID:  id,
					got:        length,
					allowedMax: piece.BlockSize,
				}
				return
			}
			var buf bufferpool.Buffer
			buf, err = p.readPiece(length)
			if err != nil {
				return
			}
			msg = Piece{PieceMessage: pm, Buffer: buf}
		case peerprotocol.HaveAll:
			msg = peerprotocol.HaveAllMessage{}
		case peerprotocol.HaveNone:
			msg = peerprotocol.HaveNoneMessage{}
		case peerprotocol.AllowedFast:
			var am peerprotocol.AllowedFastMessage
			err = binary.Read(p.r, binary.BigEndian, &am)
			if err != nil {
				return
			}
			msg = am
		case peerprotocol.Port:
			var pm peerprotocol.PortMessage
			err = binary.Read(p.r, binary.BigEndian, &pm)
			if err != nil {
				return
			}
			msg = pm
		case peerprotocol.Extension:
			buf := make([]byte, length)
			_, err = io.ReadFull(p.r, buf)
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
			_, err = io.CopyN(ioutil.Discard, p.r, int64(length))
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

func (p *PeerReader) readPiece(length uint32) (buf bufferpool.Buffer, err error) {
	buf = blockPool.Get(int(length))
	defer func() {
		if err != nil {
			buf.Release()
		}
	}()

	var n, m int
	for {
		if p.bucket != nil {
			d := p.bucket.Take(int64(length))
			select {
			case <-time.After(d):
			case <-p.stopC:
				err = errStoppedWhileWaitingBucket
				return
			}
		}

		err = p.conn.SetReadDeadline(time.Now().Add(p.pieceTimeout))
		if err != nil {
			return
		}
		n, err = io.ReadFull(p.r, buf.Data[m:])
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				// Peer didn't send the full block in allowed time.
				if n > 0 {
					// Some bytes received, peer appears to be slow, keep receiving the rest.
					m += n
					continue
				}
				// Disconnect if no bytes received.
				return
			}
			// Error other than timeout
			return
		}
		// Received full block.
		return
	}
}

var errStoppedWhileWaitingBucket = errors.New("peer reader stopped while waiting for bucket")

type blockSizeError struct {
	messageID  peerprotocol.MessageID
	got        uint32
	allowedMax uint32
}

func (e *blockSizeError) Error() string {
	return fmt.Sprintf("received %s message with block size larger than allowed (%d > %d)", e.messageID, e.got, e.allowedMax)
}
