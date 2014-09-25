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

	"github.com/zeebo/bencode"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/piece"
	"github.com/cenkalti/rain/internal/protocol"
)

const connReadTimeout = 3 * time.Minute

const (
	Outgoing = iota
	Incoming
)

type Peer struct {
	conn net.Conn

	// Will be closed when peer disconnects
	Disconnected chan struct{}

	transfer   Transfer
	downloader Downloader
	uploader   Uploader

	amChoking      bool // this client is choking the peer
	amInterested   bool // this client is interested in the peer
	peerChoking    bool // peer is choking this client
	peerInterested bool // peer is interested in this client

	onceInterested sync.Once // for sending "interested" message only once

	unchokeWaiters  []chan struct{}
	unchokeWaitersM sync.Mutex

	log logger.Logger
}

type Transfer interface {
	BitField() bitfield.BitField
	Pieces() []*piece.Piece
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
	Piece *piece.Piece
}

type Block struct {
	Peer  *Peer
	Piece *piece.Piece
	Block *piece.Block
	Data  chan []byte
}

type Request struct {
	Peer   *Peer
	Piece  *piece.Piece
	Begin  uint32
	Length uint32
}

func New(conn net.Conn, direction int, t Transfer) *Peer {
	var arrow string
	switch direction {
	case Outgoing:
		arrow = "-> "
	case Incoming:
		arrow = "<- "
	}
	return &Peer{
		conn:         conn,
		Disconnected: make(chan struct{}),
		transfer:     t,
		downloader:   t.Downloader(),
		uploader:     t.Uploader(),
		amChoking:    true,
		peerChoking:  true,
		log:          logger.New("peer " + arrow + conn.RemoteAddr().String()),
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

		var msgType protocol.MessageType
		err = binary.Read(p.conn, binary.BigEndian, &msgType)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type %q", msgType)

		switch msgType {
		case protocol.Choke:
			p.peerChoking = true
		case protocol.Unchoke:
			p.unchokeWaitersM.Lock()
			p.peerChoking = false
			for _, ch := range p.unchokeWaiters {
				close(ch)
			}
			p.unchokeWaiters = nil
			p.unchokeWaitersM.Unlock()
		case protocol.Interested:
			p.peerInterested = true
			if err := p.sendMessage(protocol.Unchoke, nil); err != nil {
				p.log.Error(err)
				return
			}
		case protocol.NotInterested:
			p.peerInterested = false
		case protocol.Have:
			var i uint32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				p.log.Error(err)
				return
			}
			if i >= uint32(len(p.transfer.Pieces())) {
				p.log.Error("unexpected piece index")
				return
			}
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.downloader.HaveC() <- &Have{p, p.transfer.Pieces()[i]}
		case protocol.Bitfield:
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
					p.downloader.HaveC() <- &Have{p, p.transfer.Pieces()[i]}
				}
			}
		case protocol.Request:
			var req requestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Request: %#v", req)

			if req.Index >= uint32(len(p.transfer.Pieces())) {
				p.log.Error("invalid request: index")
				return
			}
			requestedPiece := p.transfer.Pieces()[req.Index]
			if req.Begin >= requestedPiece.Length() {
				p.log.Error("invalid request: begin")
				return
			}
			if req.Length > protocol.MaxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > requestedPiece.Length() {
				p.log.Error("invalid request: length")
			}

			p.uploader.RequestC() <- &Request{p, requestedPiece, req.Begin, req.Length}
		case protocol.Piece:
			var msg pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			if msg.Index >= uint32(len(p.transfer.Pieces())) {
				p.log.Error("unexpected piece index")
				return
			}
			receivedPiece := p.transfer.Pieces()[msg.Index]
			if msg.Begin%protocol.BlockSize != 0 {
				p.log.Error("unexpected piece offset")
				return
			}
			blockIndex := msg.Begin / protocol.BlockSize
			if blockIndex >= uint32(len(receivedPiece.Blocks())) {
				p.log.Error("unexpected piece offset")
				return
			}
			block := &receivedPiece.Blocks()[blockIndex]
			length -= 8
			if length != block.Length() {
				p.log.Error("unexpected block size")
				return
			}
			dataC := make(chan []byte, 1)
			p.downloader.BlockC() <- &Block{p, receivedPiece, block, dataC}
			data := make([]byte, length)
			_, err = io.ReadFull(p.conn, data)
			if err != nil {
				p.log.Error(err)
				dataC <- nil
				return
			}
			dataC <- data
		case protocol.Cancel:
		case protocol.Port:
		default:
			p.log.Debugf("Unknown message type: %d", msgType)
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
	return p.sendMessage(protocol.Bitfield, p.transfer.BitField().Bytes())
}

// BeInterested sends "interested" message to peer (once) and
// returns a channel that will be closed when an "unchoke" message is received.
func (p *Peer) BeInterested() (unchoke chan struct{}, err error) {
	p.log.Debug("BeInterested")

	p.unchokeWaitersM.Lock()
	defer p.unchokeWaitersM.Unlock()

	p.onceInterested.Do(func() { err = p.sendMessage(protocol.Interested, nil) })
	if err != nil {
		return
	}

	unchoke = make(chan struct{})
	p.unchokeWaiters = append(p.unchokeWaiters, unchoke)
	return
}

func (p *Peer) Request(index, begin, length uint32) error {
	request := requestMessage{
		index, begin, length,
	}
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	binary.Write(buf, binary.BigEndian, &request)
	return p.sendMessage(protocol.Request, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {

	// TODO not here
	if err := p.sendMessage(protocol.Unchoke, nil); err != nil {
		return err
	}

	msg := &pieceMessage{index, begin}
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.sendMessage(protocol.Piece, buf.Bytes())
}

func (p *Peer) sendMessage(id protocol.MessageType, payload []byte) error {
	buf := bufio.NewWriterSize(p.conn, 4+1+len(payload))
	var header = struct {
		Length uint32
		ID     protocol.MessageType
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

type ExtensionHandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	MetadataSize uint32           `bencode:"metadata_size,omitempty"`
}

func (p *Peer) SendExtensionHandshake(m *ExtensionHandshakeMessage) error {
	const extensionHandshakeID = 0
	var buf bytes.Buffer
	e := bencode.NewEncoder(&buf)
	err := e.Encode(m)
	if err != nil {
		return err
	}
	return p.sendExtensionMessage(extensionHandshakeID, buf.Bytes())
}

func (p *Peer) sendExtensionMessage(id byte, payload []byte) error {
	msg := struct {
		Length      uint32
		BTID        byte
		ExtensionID byte
	}{
		Length:      uint32(len(payload)) + 2,
		BTID:        protocol.Extension,
		ExtensionID: id,
	}

	buf := bytes.NewBuffer(make([]byte, 0, 6+len(payload)))
	err := binary.Write(buf, binary.BigEndian, msg)
	if err != nil {
		return err
	}

	_, err = buf.Write(payload)
	if err != nil {
		return err
	}

	return binary.Write(p.conn, binary.BigEndian, buf.Bytes())
}
