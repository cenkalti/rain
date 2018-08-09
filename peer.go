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

	"github.com/cenkalti/rain/bitfield"
	"github.com/cenkalti/rain/logger"
	"github.com/cenkalti/rain/messageid"
	"github.com/cenkalti/rain/piece"
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type peer struct {
	conn    net.Conn
	id      [20]byte
	torrent *Torrent

	disconnected bool
	amInterested bool
	peerChoking  bool

	amInterestedM sync.Mutex

	// pieces that the peer has
	bitfield *bitfield.Bitfield

	cond *sync.Cond
	log  logger.Logger
}

type peerRequest struct {
	Peer *peer
	requestMessage
}
type requestMessage struct {
	Index, Begin, Length uint32
}

type pieceMessage struct {
	Index, Begin uint32
}

func (t *Torrent) newPeer(conn net.Conn, id [20]byte, l logger.Logger) *peer {
	p := &peer{
		conn:        conn,
		id:          id,
		torrent:     t,
		peerChoking: true,
		bitfield:    bitfield.New(t.bitfield.Len()),
		log:         l,
	}
	p.cond = sync.NewCond(&t.m)
	return p
}

func (p *peer) String() string { return p.conn.RemoteAddr().String() }
func (p *peer) Close() error   { return p.conn.Close() }

// Run reads and processes incoming messages after handshake.
// TODO send keep-alive messages to peers at interval.
func (p *peer) Run() {
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	if err := p.SendBitfield(); err != nil {
		p.log.Error(err)
		return
	}

	go p.downloader()

	defer func() {
		for i := uint32(0); i < p.bitfield.Len(); i++ {
			if p.bitfield.Test(i) {
				delete(p.torrent.pieces[i].Peers, p.id)
			}
		}
	}()

	defer func() {
		p.torrent.m.Lock()
		p.disconnected = true
		p.torrent.m.Unlock()
		p.cond.Broadcast()
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
			p.torrent.m.Lock()
			// Discard all pending requests. TODO
			p.peerChoking = true
			p.torrent.m.Unlock()
			p.cond.Broadcast()
		case messageid.Unchoke:
			p.torrent.m.Lock()
			p.peerChoking = false
			p.torrent.m.Unlock()
			p.cond.Broadcast()
		case messageid.Interested:
			// TODO this should not be here
			if err2 := p.Unchoke(); err2 != nil {
				p.log.Error(err2)
				return
			}
		case messageid.NotInterested:
		case messageid.Have:
			var i uint32
			err = binary.Read(p.conn, binary.BigEndian, &i)
			if err != nil {
				p.log.Error(err)
				return
			}
			if i >= p.torrent.info.NumPieces {
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

			if length != uint32(len(p.torrent.bitfield.Bytes())) {
				p.log.Error("invalid bitfield length")
				return
			}

			p.torrent.m.Lock()
			_, err = p.conn.Read(p.bitfield.Bytes())
			p.torrent.m.Unlock()
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugln("Received bitfield:", p.bitfield.Hex())

			for i := uint32(0); i < p.bitfield.Len(); i++ {
				if p.bitfield.Test(i) {
					p.handleHave(i)
				}
			}
		case messageid.Request:
			var req requestMessage
			err = binary.Read(p.conn, binary.BigEndian, &req)
			if err != nil {
				p.log.Error(err)
				return
			}
			p.log.Debugf("Request: %+v", req)

			if req.Index >= p.torrent.info.NumPieces {
				p.log.Error("invalid request: index")
				return
			}
			if req.Length > maxAllowedBlockSize {
				p.log.Error("received a request with block size larger than allowed")
				return
			}
			if req.Begin+req.Length > p.torrent.pieces[req.Index].Length {
				p.log.Error("invalid request: length")
			}

			p.torrent.requestC <- &peerRequest{p, req}
		case messageid.Piece:
			var msg pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			if msg.Index >= p.torrent.info.NumPieces {
				p.log.Error("invalid request: index")
				return
			}
			piece := p.torrent.pieces[msg.Index]

			// We request only in blockSize length
			blockIndex, mod := divMod32(msg.Begin, blockSize)
			if mod != 0 {
				p.log.Error("unexpected block begin")
				return
			}
			if blockIndex >= uint32(len(piece.Blocks)) {
				p.log.Error("invalid block begin")
				return
			}
			block := p.torrent.pieces[msg.Index].Blocks[blockIndex]
			if length != block.Length {
				p.log.Error("invalid piece block length")
				return
			}

			p.torrent.m.Lock()
			active := piece.GetRequest(p.id)
			if active == nil {
				p.torrent.m.Unlock()
				p.log.Warning("received a piece that is not active")
				continue
			}

			if active.BlocksReceiving.Test(block.Index) {
				p.log.Warningf("Receiving duplicate block: Piece #%d Block #%d", piece.Index, block.Index)
			} else {
				active.BlocksReceiving.Set(block.Index)
			}
			p.torrent.m.Unlock()

			if _, err = io.ReadFull(p.conn, active.Data[msg.Begin:msg.Begin+length]); err != nil {
				p.log.Error(err)
				return
			}

			p.torrent.m.Lock()
			active.BlocksReceived.Set(block.Index)
			if !active.BlocksReceived.All() {
				p.torrent.m.Unlock()
				p.cond.Broadcast()
				continue
			}
			p.torrent.m.Unlock()

			p.log.Debugf("Writing piece to disk: #%d", piece.Index)
			if _, err = piece.Write(active.Data); err != nil {
				p.log.Error(err)
				// TODO remove errcheck ignore
				p.conn.Close() // nolint: errcheck
				return
			}

			p.torrent.m.Lock()
			p.torrent.bitfield.Set(piece.Index)
			percentDone := p.torrent.bitfield.Count() * 100 / p.torrent.bitfield.Len()
			p.torrent.m.Unlock()
			p.cond.Broadcast()
			p.torrent.log.Infof("Completed: %d%%", percentDone)
		case messageid.Cancel:
		case messageid.Port:
		default:
			p.log.Debugf("Unknown message type: %d", id)
			p.log.Debugln("Discarding", length, "bytes...")
			// TODO remove errcheck ignore
			io.CopyN(ioutil.Discard, p.conn, int64(length)) // nolint: errcheck
			p.log.Debug("Discarding finished.")
		}

		first = false
	}
}

func (p *peer) handleHave(i uint32) {
	p.torrent.m.Lock()
	p.torrent.pieces[i].Peers[p.id] = struct{}{}
	p.torrent.m.Unlock()
	p.cond.Broadcast()
}

func (p *peer) SendBitfield() error {
	// Do not send a bitfield message if we don't have any pieces.
	if p.torrent.bitfield.Count() == 0 {
		return nil
	}
	return p.sendMessage(messageid.Bitfield, p.torrent.bitfield.Bytes())
}

func (p *peer) BeInterested() error {
	p.amInterestedM.Lock()
	defer p.amInterestedM.Unlock()
	if p.amInterested {
		return nil
	}
	p.amInterested = true
	return p.sendMessage(messageid.Interested, nil)
}

func (p *peer) BeNotInterested() error {
	p.amInterestedM.Lock()
	defer p.amInterestedM.Unlock()
	if !p.amInterested {
		return nil
	}
	p.amInterested = false
	return p.sendMessage(messageid.NotInterested, nil)
}

func (p *peer) Choke() error   { return p.sendMessage(messageid.Choke, nil) }
func (p *peer) Unchoke() error { return p.sendMessage(messageid.Unchoke, nil) }

func (p *peer) Request(b *piece.Block) error {
	req := requestMessage{b.Piece.Index, b.Begin, b.Length}
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	// TODO remove errcheck ignore
	binary.Write(buf, binary.BigEndian, &req) // nolint: errcheck
	return p.sendMessage(messageid.Request, buf.Bytes())
}

func (p *peer) SendPiece(index, begin uint32, block []byte) error {
	msg := &pieceMessage{index, begin}
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	// TODO remove errcheck ignore
	binary.Write(buf, binary.BigEndian, msg) // nolint: errcheck
	buf.Write(block)
	return p.sendMessage(messageid.Piece, buf.Bytes())
}

func (p *peer) sendMessage(id messageid.MessageID, payload []byte) error {
	p.log.Debugf("Sending message of type: %q", id)
	buf := bufio.NewWriterSize(p.conn, 4+1+len(payload))
	var header = struct {
		Length uint32
		ID     messageid.MessageID
	}{
		uint32(1 + len(payload)),
		id,
	}
	// TODO remove errcheck ignore
	binary.Write(buf, binary.BigEndian, &header) // nolint: errcheck
	buf.Write(payload)                           // nolint: errcheck
	return buf.Flush()
}

func divMod32(a, b uint32) (uint32, uint32) { return a / b, a % b }
