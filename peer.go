package rain

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/log"

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/bt"
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type Peer struct {
	// Will be closed when peer disconnects
	Disconnected chan struct{}

	conn     net.Conn
	peerID   bt.PeerID
	transfer *transfer

	amInterested bool

	// pieces that the peer has
	bitfield bitfield.BitField

	// TODO write comment here
	haveNewPiece chan struct{}

	pieceC chan *PieceMessage

	// requests that we made
	requests  map[requestMessage]struct{}
	requestsM sync.Mutex

	log log.Logger
}

type Request struct {
	Peer *Peer
	requestMessage
}
type requestMessage struct {
	Index, Begin, Length uint32
}

type PieceMessage struct {
	pieceMessage
	Data chan []byte
}
type pieceMessage struct {
	Index, Begin uint32
}

func NewPeer(conn net.Conn, peerID bt.PeerID, t *transfer, l log.Logger) *Peer {
	return &Peer{
		Disconnected: make(chan struct{}),
		conn:         conn,
		peerID:       peerID,
		transfer:     t,
		bitfield:     bitfield.New(t.bitfield.Len()),
		haveNewPiece: make(chan struct{}, 1),
		pieceC:       make(chan *PieceMessage),
		requests:     make(map[requestMessage]struct{}),
		log:          l,
	}
}

func (p *Peer) String() string { return p.conn.RemoteAddr().String() }
func (p *Peer) Close() error   { return p.conn.Close() }

// Run reads and processes incoming messages after handshake.
func (p *Peer) Run() {
	defer close(p.Disconnected)
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	first := true
	for {
		err := p.conn.SetReadDeadline(time.Now().Add(connReadTimeout))
		if err != nil {
			p.log.Error(err)
			return
		}

		var length uint32
		p.log.Debug("Reading message...")
		err = binary.Read(p.conn, binary.BigEndian, &length)
		if err != nil {
			if err == io.EOF {
				p.log.Warning("Remote peer has closed the connection")
				return
			}
			p.log.Error(err)
			return
		}
		p.log.Debugf("Received message of length: %d", length)

		if length == 0 { // keep-alive message
			p.log.Debug("Received message of type \"keep alive\"")
			continue
		}

		var id messageID
		err = binary.Read(p.conn, binary.BigEndian, &id)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type %q", id)

		switch id {
		case chokeID:
			// Discard all pending requests.
			p.requestsM.Lock()
			p.requests = make(map[requestMessage]struct{})
			p.requestsM.Unlock()
			// TODO send message to chokeC
		case unchokeID:
		case interestedID:
		case notInterestedID:
		case haveID:
			var i uint32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				p.log.Error(err)
				return
			}
			if i >= p.transfer.torrent.Info.NumPieces {
				p.log.Error("unexpected piece index")
				return
			}
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.bitfield.Set(i)
		case bitfieldID:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}

			if length != p.transfer.torrent.Info.NumPieces {
				p.log.Error("invalid bitfield length")
				return
			}

			_, err = p.conn.Read(p.bitfield.Bytes())
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugln("Received bitfield:", p.bitfield.Hex())
		case requestID:
			var req requestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Request: %+v", req)

			if req.Index >= p.transfer.torrent.Info.NumPieces {
				p.log.Error("invalid request: index")
				return
			}
			if req.Length > maxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > p.transfer.pieces[req.Index].Length {
				p.log.Error("invalid request: length")
			}

			p.transfer.requestC <- &Request{p, req}
		case pieceID:
			var piece pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &piece)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			req := requestMessage{piece.Index, piece.Begin, length}
			p.requestsM.Lock()
			if _, ok := p.requests[req]; !ok {
				p.log.Error("unexpected piece message")
				p.requestsM.Unlock()
				return
			}
			delete(p.requests, req)
			p.requestsM.Unlock()

			dataC := make(chan []byte, 1)
			p.pieceC <- &PieceMessage{piece, dataC}
			data := make([]byte, length)
			_, err = io.ReadFull(p.conn, data)
			if err != nil {
				p.log.Error(err)
				dataC <- nil
				return
			}
			dataC <- data
		case cancelID:
		case portID:
		default:
			p.log.Debugf("Unknown message type: %d", id)
			p.log.Debugln("Discarding", length, "bytes...")
			io.CopyN(ioutil.Discard, p.conn, int64(length))
			p.log.Debug("Discarding finished.")
		}

		first = false
	}
}

func (p *Peer) SendBitField() error {
	// Do not send a bitfield message if we don't have any pieces.
	if p.transfer.bitfield.Count() == 0 {
		return nil
	}
	return p.sendMessage(bitfieldID, p.transfer.bitfield.Bytes())
}

func (p *Peer) BeInterested() error {
	if p.amInterested {
		return nil
	}
	p.amInterested = true
	return p.sendMessage(interestedID, nil)
}

func (p *Peer) BeNotInterested() error {
	if !p.amInterested {
		return nil
	}
	p.amInterested = false
	return p.sendMessage(notInterestedID, nil)
}

func (p *Peer) Choke() error   { return p.sendMessage(chokeID, nil) }
func (p *Peer) Unchoke() error { return p.sendMessage(unchokeID, nil) }

func (p *Peer) Request(index, begin, length uint32) error {
	req := requestMessage{index, begin, length}
	p.requestsM.Lock()
	p.requests[req] = struct{}{}
	p.requestsM.Unlock()

	buf := bytes.NewBuffer(make([]byte, 0, 12))
	binary.Write(buf, binary.BigEndian, &req)
	return p.sendMessage(requestID, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {

	// TODO not here
	if err := p.sendMessage(unchokeID, nil); err != nil {
		return err
	}

	msg := &pieceMessage{index, begin}
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.sendMessage(pieceID, buf.Bytes())
}

func (p *Peer) sendMessage(id messageID, payload []byte) error {
	p.log.Debugln("Sending message:", id)
	buf := bufio.NewWriterSize(p.conn, 4+1+len(payload))
	var header = struct {
		Length uint32
		ID     messageID
	}{
		uint32(1 + len(payload)),
		id,
	}
	binary.Write(buf, binary.BigEndian, &header)
	buf.Write(payload)
	return buf.Flush()
}
