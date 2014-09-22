package rain

import (
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
	"github.com/cenkalti/rain/internal/protocol"
)

// All current implementations use 2^14 (16 kiB), and close connections which request an amount greater than that.
const blockSize = 16 * 1024

const (
	outgoing = iota
	incoming
)

type peer struct {
	conn         net.Conn
	disconnected chan struct{} // will be closed when peer disconnects

	amChoking      bool // this client is choking the peer
	amInterested   bool // this client is interested in the peer
	peerChoking    bool // peer is choking this client
	peerInterested bool // peer is interested in this client

	onceInterested sync.Once // for sending "interested" message only once

	// Protects "peerChoking" and broadcasts when an "unchoke" message is received.
	unchokeCond sync.Cond

	// What remote peer requested
	requests chan peerRequest
	// ourRequests    map[uint64]time.Time // What we requested, when we requested it

	log logger.Logger
}

func newPeer(conn net.Conn, direction int) *peer {
	var arrow string
	switch direction {
	case outgoing:
		arrow = "-> "
	case incoming:
		arrow = "<- "
	}
	var m sync.Mutex
	return &peer{
		conn:         conn,
		disconnected: make(chan struct{}),
		amChoking:    true,
		peerChoking:  true,
		unchokeCond:  sync.Cond{L: &m},
		requests:     make(chan peerRequest, 10),
		log:          logger.New("peer " + arrow + conn.RemoteAddr().String()),
	}
}

const connReadTimeout = 3 * time.Minute

// Serve processes incoming messages after handshake.
func (p *peer) Serve(t *transfer) {
	defer close(p.disconnected)
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	// Do not send bitfield if we don't have any pieces.
	if t.bitField.Count() > 0 {
		err := p.sendBitField(t.bitField)
		if err != nil {
			p.log.Error(err)
			return
		}
	}

	bitField := bitfield.New(t.bitField.Len())

	t.peersM.Lock()
	t.peers[p] = struct{}{}
	t.peersM.Unlock()
	defer func() {
		t.peersM.Lock()
		delete(t.peers, p)
		t.peersM.Unlock()
	}()

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
			if err := p.sendMessage(protocol.Unchoke); err != nil {
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
			if i >= uint32(len(t.pieces)) {
				p.log.Error("unexpected piece index")
				return
			}
			piece := t.pieces[i]
			bitField.Set(i)
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.log.Debugln("new bitfield:", bitField.Hex())

			t.haveC <- peerHave{p, piece}
		case protocol.Bitfield:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}

			if int64(length) != int64(len(bitField.Bytes())) {
				p.log.Error("invalid bitfield length")
				return
			}

			_, err = p.conn.Read(bitField.Bytes())
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugln("Received bitfield:", bitField.Hex())

			for i := uint32(0); i < bitField.Len(); i++ {
				if bitField.Test(i) {
					t.haveC <- peerHave{p, t.pieces[i]}
				}
			}
		case protocol.Request:
			var req request
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Request: %#v", req)

			if req.Index >= uint32(len(t.pieces)) {
				p.log.Error("invalid request: index")
				return
			}
			piece := t.pieces[req.Index]
			if req.Begin >= piece.length {
				p.log.Error("invalid request: begin")
				return
			}
			if req.Length > blockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > piece.length {
				p.log.Error("invalid request: length")
			}

			p.requests <- peerRequest{p, piece, req.Begin, req.Length}
		case protocol.Piece:
			var index uint32
			err = binary.Read(p.conn, binary.BigEndian, &index)
			if err != nil {
				p.log.Error(err)
				return
			}
			if index >= uint32(len(t.pieces)) {
				p.log.Error("unexpected piece index")
				return
			}
			piece := t.pieces[index]
			var begin uint32
			err = binary.Read(p.conn, binary.BigEndian, &begin)
			if err != nil {
				p.log.Error(err)
				return
			}
			if begin%blockSize != 0 {
				p.log.Error("unexpected piece offset")
				return
			}
			blockIndex := begin / blockSize
			if blockIndex >= uint32(len(piece.blocks)) {
				p.log.Error("unexpected piece offset")
				return
			}
			block := &piece.blocks[blockIndex]
			length -= 8
			if length != block.length {
				p.log.Error("unexpected block size")
				return
			}
			data := make([]byte, length)
			_, err = io.ReadFull(p.conn, data)
			if err != nil {
				p.log.Error(err)
				return
			}
			piece.blockC <- peerBlock{p, block, data}
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

func (p *peer) sendBitField(b bitfield.BitField) error {
	buf := bytes.NewBuffer(make([]byte, 0, 5+len(b.Bytes())))
	err := binary.Write(buf, binary.BigEndian, uint32(1+len(b.Bytes())))
	if err != nil {
		return err
	}
	if err = buf.WriteByte(byte(protocol.Bitfield)); err != nil {
		return err
	}
	if _, err = buf.Write(b.Bytes()); err != nil {
		return err
	}
	p.log.Debugf("Sending message: \"bitfield\" %#v", buf.Bytes())
	_, err = buf.WriteTo(p.conn)
	return err
}

// beInterested sends "interested" message to peer (once) and
// returns a channel that will be closed when an "unchoke" message is received.
func (p *peer) beInterested() error {
	p.log.Debug("beInterested")

	var err error
	p.onceInterested.Do(func() { err = p.sendMessage(protocol.Interested) })
	if err != nil {
		return err
	}

	var disconnected bool
	checkDisconnect := func() {
		select {
		case <-p.disconnected:
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

func (p *peer) sendMessage(msgType protocol.MessageType) error {
	var msg = struct {
		Length      uint32
		MessageType protocol.MessageType
	}{1, msgType}
	p.log.Debugf("Sending message: %q", msgType)
	return binary.Write(p.conn, binary.BigEndian, &msg)
}

func (p *peer) sendExtensionMessage(id byte, payload []byte) error {
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

func (p *peer) sendExtensionHandshake(m *extensionHandshakeMessage) error {
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

func newPeerRequestMessage(index, begin, length uint32) *peerRequestMessage {
	return &peerRequestMessage{protocol.Request, index, begin, length}
}

func (p *peer) sendRequest(m *peerRequestMessage) error {
	var msg = struct {
		Length  uint32
		Message peerRequestMessage
	}{13, *m}
	p.log.Debugf("Sending message: %q %#v", "request", msg)
	return binary.Write(p.conn, binary.BigEndian, &msg)
}

func (p *peer) downloadPiece(piece *piece) error {
	p.log.Debugf("downloading piece #%d", piece.index)

	err := p.beInterested()
	if err != nil {
		return err
	}

	for _, b := range piece.blocks {
		if err := p.sendRequest(newPeerRequestMessage(piece.index, b.index*blockSize, b.length)); err != nil {
			return err
		}
	}

	pieceData := make([]byte, piece.length)
	for _ = range piece.blocks {
		select {
		case peerBlock := <-piece.blockC:
			p.log.Debugln("received block of length", len(peerBlock.data))
			copy(pieceData[peerBlock.block.index*blockSize:], peerBlock.data)
			if _, err = peerBlock.block.files.Write(peerBlock.data); err != nil {
				return err
			}
			piece.bitField.Set(peerBlock.block.index)
		case <-time.After(time.Minute):
			return fmt.Errorf("peer did not send piece #%d completely", piece.index)
		}
	}

	// Verify piece hash
	hash := sha1.New()
	hash.Write(pieceData)
	if !bytes.Equal(hash.Sum(nil), piece.hash) {
		return errors.New("received corrupt piece")
	}

	piece.log.Debug("piece written successfully")
	return nil
}

type peerHave struct {
	peer  *peer
	piece *piece
}

type peerBlock struct {
	peer  *peer
	block *block
	data  []byte
}

type peerRequest struct {
	peer   *peer
	piece  *piece
	begin  uint32
	length uint32
}

type request struct {
	Index, Begin, Length uint32
}
