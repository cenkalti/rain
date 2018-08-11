package peer

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/logger"
	"github.com/cenkalti/rain/internal/messageid"
	"github.com/cenkalti/rain/internal/piece"
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type Peer struct {
	conn net.Conn
	id   [20]byte

	amChoking      bool
	amInterested   bool
	peerChoking    bool
	peerInterested bool

	// pieces that the peer has
	bitfield *bitfield.Bitfield

	// pieces we have
	have *bitfield.Bitfield

	pending *bitfield.Bitfield

	// cond *sync.Cond
	m   sync.Mutex
	log logger.Logger
}

type requestMessage struct {
	Index, Begin, Length uint32
}

type pieceMessage struct {
	Index, Begin uint32
}

func New(conn net.Conn, id [20]byte, have *bitfield.Bitfield, l logger.Logger) *Peer {
	p := &Peer{
		conn:        conn,
		id:          id,
		amChoking:   true,
		peerChoking: true,
		have:        have,
		log:         l,
	}
	return p
}

func (p *Peer) String() string { return p.conn.RemoteAddr().String() }
func (p *Peer) Close() error   { return p.conn.Close() }

// Run reads and processes incoming messages after handshake.
// TODO send keep-alive messages to peers at interval.
func (p *Peer) Run() {
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	if err := p.SendBitfield(); err != nil {
		p.log.Error(err)
		return
	}

	first := true
	for {
		err := p.conn.SetReadDeadline(time.Now().Add(connReadTimeout))
		if err != nil {
			p.log.Error(err)
			return
		}

		// p.

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

		var id messageid.MessageID
		err = binary.Read(p.conn, binary.BigEndian, &id)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type: %q", id)

		switch id {
		case messageid.Choke:
			p.m.Lock()
			// Discard all pending requests. TODO
			p.peerChoking = true
			p.m.Unlock()
			// p.cond.Broadcast()
		case messageid.Unchoke:
			p.m.Lock()
			p.peerChoking = false
			p.m.Unlock()
			// p.cond.Broadcast()
		case messageid.Interested:
			p.m.Lock()
			p.peerInterested = true
			p.m.Unlock()
			// TODO implement
			// // TODO this should not be here
			// if err2 := p.Unchoke(); err2 != nil {
			// 	p.log.Error(err2)
			// 	return
			// }
		case messageid.NotInterested:
			p.m.Lock()
			p.peerInterested = false
			p.m.Unlock()
		case messageid.Have:
			var i uint32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				p.log.Error(err)
				return
			}
			if i >= p.have.Len() {
				p.log.Error("unexpected piece index")
				return
			}
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.bitfield.Set(i)
			p.handleHave(i)
		case messageid.Bitfield:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}

			if length != uint32(len(p.have.Bytes())) {
				p.log.Error("invalid bitfield length")
				return
			}

			b := make([]byte, len(p.have.Bytes()))
			_, err = io.ReadFull(p.conn, b)
			if err != nil {
				p.log.Error(err)
				return
			}
			bf := bitfield.NewBytes(b, p.have.Len())
			p.m.Lock()
			p.bitfield = bf
			p.m.Unlock()
			p.log.Debugln("Received bitfield:", p.bitfield.Hex())

			for i := uint32(0); i < p.bitfield.Len(); i++ {
				if p.bitfield.Test(i) {
					p.handleHave(i)
				}
			}
		// 	case messageid.Request:
		// 		var req requestMessage
		// 		err = binary.Read(p.conn, binary.BigEndian, &req)
		// 		if err != nil {
		// 			p.log.Error(err)
		// 			return
		// 		}
		// 		p.log.Debugf("Request: %+v", req)

		// 		if req.Index >= p.torrent.info.NumPieces {
		// 			p.log.Error("invalid request: index")
		// 			return
		// 		}
		// 		if req.Length > maxAllowedBlockSize {
		// 			p.log.Error("received a request with block size larger than allowed")
		// 			return
		// 		}
		// 		if req.Begin+req.Length > p.torrent.pieces[req.Index].Length {
		// 			p.log.Error("invalid request: length")
		// 		}

		// 		p.torrent.requestC <- &peerRequest{p, req}
		// 	case messageid.Piece:
		// 		var msg pieceMessage
		// 		err = binary.Read(p.conn, binary.BigEndian, &msg)
		// 		if err != nil {
		// 			p.log.Error(err)
		// 			return
		// 		}
		// 		length -= 8

		// 		if msg.Index >= p.torrent.info.NumPieces {
		// 			p.log.Error("invalid request: index")
		// 			return
		// 		}
		// 		piece := p.torrent.pieces[msg.Index]

		// 		// We request only in blockSize length
		// 		blockIndex, mod := divMod32(msg.Begin, blockSize)
		// 		if mod != 0 {
		// 			p.log.Error("unexpected block begin")
		// 			return
		// 		}
		// 		if blockIndex >= uint32(len(piece.Blocks)) {
		// 			p.log.Error("invalid block begin")
		// 			return
		// 		}
		// 		block := p.torrent.pieces[msg.Index].Blocks[blockIndex]
		// 		if length != block.Length {
		// 			p.log.Error("invalid piece block length")
		// 			return
		// 		}

		// 		p.torrent.m.Lock()
		// 		active := piece.GetRequest(p.id)
		// 		if active == nil {
		// 			p.torrent.m.Unlock()
		// 			p.log.Warning("received a piece that is not active")
		// 			continue
		// 		}

		// 		if active.BlocksReceiving.Test(block.Index) {
		// 			p.log.Warningf("Receiving duplicate block: Piece #%d Block #%d", piece.Index, block.Index)
		// 		} else {
		// 			active.BlocksReceiving.Set(block.Index)
		// 		}
		// 		p.torrent.m.Unlock()

		// 		if _, err = io.ReadFull(p.conn, active.Data[msg.Begin:msg.Begin+length]); err != nil {
		// 			p.log.Error(err)
		// 			return
		// 		}

		// 		p.torrent.m.Lock()
		// 		active.BlocksReceived.Set(block.Index)
		// 		if !active.BlocksReceived.All() {
		// 			p.torrent.m.Unlock()
		// 			p.cond.Broadcast()
		// 			continue
		// 		}
		// 		p.torrent.m.Unlock()

		// 		p.log.Debugf("Writing piece to disk: #%d", piece.Index)
		// 		if _, err = piece.Write(active.Data); err != nil {
		// 			p.log.Error(err)
		// 			// TODO remove errcheck ignore
		// 			p.conn.Close() // nolint: errcheck
		// 			return
		// 		}

		// 		p.torrent.m.Lock()
		// 		p.torrent.bitfield.Set(piece.Index)
		// 		percentDone := p.torrent.bitfield.Count() * 100 / p.torrent.bitfield.Len()
		// 		p.torrent.m.Unlock()
		// 		p.cond.Broadcast()
		// 		p.torrent.log.Infof("Completed: %d%%", percentDone)
		// 	case messageid.Cancel:
		// 	case messageid.Port:
		default:
			p.log.Debugf("Unknown message type: %d", id)
			p.log.Debugln("Discarding", length, "bytes...")
			_, err = io.CopyN(ioutil.Discard, p.conn, int64(length))
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debug("Discarding finished.")
		}

		first = false
	}
}

func (p *Peer) handleHave(i uint32) {
	// p.torrent.m.Lock()
	// // p.torrent.pieces[i].Peers[p.id] = struct{}{}
	// p.torrent.m.Unlock()
	// p.cond.Broadcast()
	return
}

func (p *Peer) SendBitfield() error {
	// Sending bitfield may be omitted if have no pieces.
	if p.have.Count() == 0 {
		return nil
	}
	return p.sendMessage(messageid.Bitfield, p.have.Bytes())
}

func (p *Peer) BeInterested() error {
	p.m.Lock()
	if p.amInterested {
		p.m.Unlock()
		return nil
	}
	p.amInterested = true
	p.m.Unlock()
	return p.sendMessage(messageid.Interested, nil)
}

func (p *Peer) BeNotInterested() error {
	p.m.Lock()
	if !p.amInterested {
		p.m.Unlock()
		return nil
	}
	p.amInterested = false
	p.m.Unlock()
	return p.sendMessage(messageid.NotInterested, nil)
}

func (p *Peer) Choke() error {
	p.m.Lock()
	if p.amChoking {
		p.m.Unlock()
		return nil
	}
	p.amChoking = true
	p.m.Unlock()
	return p.sendMessage(messageid.Choke, nil)
}

func (p *Peer) Unchoke() error {
	p.m.Lock()
	if !p.amChoking {
		p.m.Unlock()
		return nil
	}
	p.amChoking = false
	p.m.Unlock()
	return p.sendMessage(messageid.Unchoke, nil)
}

func (p *Peer) Request(b *piece.Block) error {
	req := requestMessage{b.Piece.Index, b.Begin, b.Length}
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	// TODO remove errcheck ignore
	binary.Write(buf, binary.BigEndian, &req) // nolint: errcheck
	return p.sendMessage(messageid.Request, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {
	msg := &pieceMessage{index, begin}
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	// TODO remove errcheck ignore
	binary.Write(buf, binary.BigEndian, msg) // nolint: errcheck
	buf.Write(block)
	return p.sendMessage(messageid.Piece, buf.Bytes())
}

func (p *Peer) sendMessage(id messageid.MessageID, payload []byte) error {
	p.log.Debugf("Sending message of type: %q", id)
	buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
	var header = struct {
		Length uint32
		ID     messageid.MessageID
	}{
		uint32(1 + len(payload)),
		id,
	}
	_ = binary.Write(buf, binary.BigEndian, &header)
	_, err := p.conn.Write(buf.Bytes())
	return err
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
