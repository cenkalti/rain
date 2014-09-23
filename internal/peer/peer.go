package peer

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
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

	// Protects "peerChoking" and broadcasts when an "unchoke" message is received.
	unchokeCond sync.Cond

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
	Data  []byte
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
	var m sync.Mutex
	return &Peer{
		conn:         conn,
		Disconnected: make(chan struct{}),
		transfer:     t,
		downloader:   t.Downloader(),
		uploader:     t.Uploader(),
		amChoking:    true,
		peerChoking:  true,
		unchokeCond:  sync.Cond{L: &m},
		log:          logger.New("peer " + arrow + conn.RemoteAddr().String()),
	}
}

// Serve processes incoming messages after handshake.
func (p *Peer) Serve() {
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
			p.unchokeCond.L.Lock()
			p.peerChoking = true
			p.unchokeCond.L.Unlock()
		case protocol.Unchoke:
			p.unchokeCond.L.Lock()
			p.peerChoking = false
			p.unchokeCond.Broadcast()
			p.unchokeCond.L.Unlock()
		case protocol.Interested:
			p.peerInterested = true
			if err := p.SendMessage(protocol.Unchoke); err != nil {
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
			data := make([]byte, length)
			_, err = io.ReadFull(p.conn, data)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.downloader.BlockC() <- &Block{p, receivedPiece, block, data}
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

	buf := bytes.NewBuffer(make([]byte, 0, 5+len(p.transfer.BitField().Bytes())))

	err := binary.Write(buf, binary.BigEndian, uint32(1+len(p.transfer.BitField().Bytes())))
	if err != nil {
		return err
	}
	if err = buf.WriteByte(byte(protocol.Bitfield)); err != nil {
		return err
	}
	if _, err = buf.Write(p.transfer.BitField().Bytes()); err != nil {
		return err
	}
	p.log.Debugf("Sending message: \"bitfield\" %#v", buf.Bytes())
	_, err = buf.WriteTo(p.conn)
	return err
}

// beInterested sends "interested" message to peer (once) and
// returns a channel that will be closed when an "unchoke" message is received.
func (p *Peer) beInterested() error {
	p.log.Debug("beInterested")

	var err error
	p.onceInterested.Do(func() { err = p.SendMessage(protocol.Interested) })
	if err != nil {
		return err
	}

	var disconnected bool
	checkDisconnect := func() {
		select {
		case <-p.Disconnected:
			disconnected = true
		default:
		}
	}

	p.unchokeCond.L.Lock()
	for checkDisconnect(); p.peerChoking && !disconnected; {
		p.unchokeCond.Wait()
	}
	p.unchokeCond.L.Unlock()

	if disconnected {
		return errors.New("peer disconnected while waiting for unchoke message")
	}

	return nil
}

func (p *Peer) SendMessage(msgType protocol.MessageType) error {
	var msg = struct {
		Length      uint32
		MessageType protocol.MessageType
	}{1, msgType}
	p.log.Debugf("Sending message: %q", msgType)
	return binary.Write(p.conn, binary.BigEndian, &msg)
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

type peerRequestMessage struct {
	ID                   protocol.MessageType
	Index, Begin, Length uint32
}

func newRequestMessage(index, begin, length uint32) *peerRequestMessage {
	return &peerRequestMessage{protocol.Request, index, begin, length}
}

func (p *Peer) sendRequest(m *peerRequestMessage) error {
	var msg = struct {
		Length  uint32
		Message peerRequestMessage
	}{13, *m}
	p.log.Debugf("Sending message: %q %#v", "request", msg)
	return binary.Write(p.conn, binary.BigEndian, &msg)
}

func (p *Peer) DownloadPiece(piece *piece.Piece) error {
	p.log.Debugf("downloading piece #%d", piece.Index())

	err := p.beInterested()
	if err != nil {
		return err
	}

	for _, b := range piece.Blocks() {
		if err := p.sendRequest(newRequestMessage(piece.Index(), b.Index()*protocol.BlockSize, b.Length())); err != nil {
			return err
		}
	}

	pieceData := make([]byte, piece.Length())
	for _ = range piece.Blocks() {
		select {
		case peerBlock := <-p.downloader.BlockC():
			p.log.Debugln("received block of length", len(peerBlock.Data))
			copy(pieceData[peerBlock.Block.Index()*protocol.BlockSize:], peerBlock.Data)
			if _, err = peerBlock.Block.Write(peerBlock.Data); err != nil {
				return err
			}
			piece.BitField().Set(peerBlock.Block.Index())
		case <-time.After(time.Minute):
			return fmt.Errorf("peer did not send piece #%d completely", piece.Index())
		}
	}

	// Verify piece hash
	hash := sha1.Sum(pieceData)
	if !bytes.Equal(hash[:], piece.Hash()) {
		return errors.New("received corrupt piece")
	}
	return nil
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {

	// TODO not here
	if err := p.SendMessage(protocol.Unchoke); err != nil {
		return err
	}

	buf := bufio.NewWriterSize(p.conn, int(13+len(block)))
	msgLen := 9 + uint32(len(block))
	if err := binary.Write(buf, binary.BigEndian, msgLen); err != nil {
		return err
	}
	buf.WriteByte(byte(protocol.Piece))
	if err := binary.Write(buf, binary.BigEndian, index); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.BigEndian, begin); err != nil {
		return err
	}
	buf.Write(block)
	return buf.Flush()
}

type requestMessage struct {
	Index, Begin, Length uint32
}

type pieceMessage struct {
	Index, Begin uint32
}
