package peer

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
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type Peer struct {
	// Will be closed when peer disconnects
	Disconnected chan struct{}

	conn net.Conn

	transfer   Transfer
	downloader Downloader
	uploader   Uploader

	// for sending "interested" message only once
	onceInterested sync.Once

	unchokeWaiters  []chan struct{}
	unchokeWaitersM sync.Mutex

	// requests that we made
	requests  map[requestMessage]struct{}
	requestsM sync.Mutex

	log log.Logger
}

type Transfer interface {
	BitField() bitfield.BitField
	PieceLength(i uint32) uint32
	Downloader() Downloader
	Uploader() Uploader
}

type Downloader interface {
	HaveC() chan *Have
	BlockC() chan *Block
}

type Uploader interface {
	RequestC() chan *Request
}

type Have struct {
	Peer  *Peer
	Index uint32
}

type Block struct {
	Peer  *Peer
	Index uint32
	Begin uint32
	Data  chan []byte
}

type Request struct {
	Peer   *Peer
	Index  uint32
	Begin  uint32
	Length uint32
}

func New(conn net.Conn, t Transfer, l log.Logger) *Peer {
	return &Peer{
		conn:         conn,
		Disconnected: make(chan struct{}),
		transfer:     t,
		downloader:   t.Downloader(),
		uploader:     t.Uploader(),
		requests:     make(map[requestMessage]struct{}),
		log:          l,
	}
}

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
			// TODO notify waiters
		case unchokeID:
			p.unchokeWaitersM.Lock()
			for _, ch := range p.unchokeWaiters {
				close(ch)
			}
			p.unchokeWaiters = nil
			p.unchokeWaitersM.Unlock()
		case interestedID:
			// TODO uploader should do this
			if err := p.sendMessage(unchokeID, nil); err != nil {
				p.log.Error(err)
				return
			}
		case notInterestedID:
		case haveID:
			var i uint32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				p.log.Error(err)
				return
			}
			if i >= p.transfer.BitField().Len() {
				p.log.Error("unexpected piece index")
				return
			}
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.downloader.HaveC() <- &Have{p, i}
		case bitfieldID:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}

			if int64(length) != int64(len(p.transfer.BitField().Bytes())) {
				p.log.Error("invalid bitfield length")
				return
			}

			bf := bitfield.New(p.transfer.BitField().Len())
			_, err = p.conn.Read(bf.Bytes())
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugln("Received bitfield:", bf.Hex())

			for i := uint32(0); i < bf.Len(); i++ {
				if bf.Test(i) {
					p.downloader.HaveC() <- &Have{p, i}
				}
			}
		case requestID:
			var req requestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Request: %#v", req)

			if req.Index >= p.transfer.BitField().Len() {
				p.log.Error("invalid request: index")
				return
			}
			if req.Length > maxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > p.transfer.PieceLength(req.Index) {
				p.log.Error("invalid request: length")
			}

			p.uploader.RequestC() <- &Request{p, req.Index, req.Begin, req.Length}
		case pieceID:
			var msg pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			req := requestMessage{msg.Index, msg.Begin, length}
			p.requestsM.Lock()
			if _, ok := p.requests[req]; !ok {
				p.log.Error("unexpected piece message")
				p.requestsM.Unlock()
				return
			}
			delete(p.requests, req)
			p.requestsM.Unlock()

			dataC := make(chan []byte, 1)
			p.downloader.BlockC() <- &Block{p, msg.Index, msg.Begin, dataC}
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
	if p.transfer.BitField().Count() == 0 {
		return nil
	}
	return p.sendMessage(bitfieldID, p.transfer.BitField().Bytes())
}

// BeInterested sends "interested" message to peer (once) and
// returns a channel that will be closed when an "unchoke" message is received.
func (p *Peer) BeInterested() (unchoke chan struct{}, err error) {
	p.log.Debug("BeInterested")

	p.unchokeWaitersM.Lock()
	defer p.unchokeWaitersM.Unlock()

	p.onceInterested.Do(func() { err = p.sendMessage(interestedID, nil) })
	if err != nil {
		return
	}

	unchoke = make(chan struct{})
	p.unchokeWaiters = append(p.unchokeWaiters, unchoke)
	return
}

func (p *Peer) BeNotInterested() error { return p.sendMessage(notInterestedID, nil) }
func (p *Peer) Choke() error           { return p.sendMessage(chokeID, nil) }
func (p *Peer) Unchoke() error         { return p.sendMessage(unchokeID, nil) }

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

type requestMessage struct {
	Index, Begin, Length uint32
}
type pieceMessage struct {
	Index, Begin uint32
}
