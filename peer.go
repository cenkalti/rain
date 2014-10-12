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
)

const connReadTimeout = 3 * time.Minute

// Reject requests larger than this size.
const maxAllowedBlockSize = 32 * 1024

type peer struct {
	conn     net.Conn
	id       PeerID
	transfer *transfer

	disconnected bool
	amInterested bool
	peerChoking  bool

	amInterestedM sync.Mutex

	// pieces that the peer has
	bitfield *bitfield

	cond *sync.Cond
	log  logger
}

type peerRequest struct {
	Peer *peer
	requestMessage
}
type requestMessage struct {
	Index, Begin, Length uint32
}

type pieceData struct {
	pieceMessage
	Data chan []byte
}
type pieceMessage struct {
	Index, Begin uint32
}

func (t *transfer) newPeer(conn net.Conn, id PeerID, l logger) *peer {
	p := &peer{
		conn:        conn,
		id:          id,
		transfer:    t,
		peerChoking: true,
		bitfield:    newBitfield(t.bitfield.Len()),
		log:         l,
	}
	p.cond = sync.NewCond(&t.m)
	return p
}

func (p *peer) String() string { return p.conn.RemoteAddr().String() }
func (p *peer) Close() error   { return p.conn.Close() }

// Run reads and processes incoming messages after handshake.
func (p *peer) Run() {
	p.log.Debugln("Communicating peer", p.conn.RemoteAddr())

	go p.downloader()

	defer func() {
		for i := uint32(0); i < p.bitfield.Len(); i++ {
			if p.bitfield.Test(i) {
				delete(p.transfer.pieces[i].peers, p.id)
			}
		}
	}()

	defer func() {
		p.transfer.m.Lock()
		p.disconnected = true
		p.transfer.m.Unlock()
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

		var id messageID
		err = binary.Read(p.conn, binary.BigEndian, &id)
		if err != nil {
			p.log.Error(err)
			return
		}
		length--

		p.log.Debugf("Received message of type: %q", id)

		switch id {
		case chokeID:
			p.transfer.m.Lock()
			// Discard all pending requests. TODO
			p.peerChoking = true
			p.transfer.m.Unlock()
			p.cond.Broadcast()
		case unchokeID:
			p.transfer.m.Lock()
			p.peerChoking = false
			p.transfer.m.Unlock()
			p.cond.Broadcast()
		case interestedID:
			// TODO this should not be here
			if err := p.Unchoke(); err != nil {
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
			if i >= p.transfer.torrent.Info.NumPieces {
				p.log.Error("unexpected piece index")
				return
			}
			p.log.Debug("Peer ", p.conn.RemoteAddr(), " has piece #", i)
			p.bitfield.Set(i)
			p.handleHave(i)
		case bitfieldID:
			if !first {
				p.log.Error("bitfield can only be sent after handshake")
				return
			}

			if length != uint32(len(p.transfer.bitfield.Bytes())) {
				p.log.Error("invalid bitfield length")
				return
			}

			p.transfer.m.Lock()
			_, err = p.conn.Read(p.bitfield.Bytes())
			p.transfer.m.Unlock()
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

			p.transfer.requestC <- &peerRequest{p, req}
		case pieceID:
			var msg pieceMessage
			err = binary.Read(p.conn, binary.BigEndian, &msg)
			if err != nil {
				p.log.Error(err)
				return
			}
			length -= 8

			if msg.Index >= p.transfer.torrent.Info.NumPieces {
				p.log.Error("invalid request: index")
				return
			}
			piece := p.transfer.pieces[msg.Index]

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
			block := p.transfer.pieces[msg.Index].Blocks[blockIndex]
			if length != block.Length {
				p.log.Error("invalid piece block length")
				return
			}

			p.transfer.m.Lock()
			active := piece.getActiveRequest(p.id)
			if active == nil {
				p.transfer.m.Unlock()
				p.log.Warning("received a piece that is not activeed")
				continue
			}

			if active.blocksReceiving.Test(block.Index) {
				p.log.Warningf("Receiving duplicate block: Piece #%d Block #%d", piece.Index, block.Index)
			} else {
				active.blocksReceiving.Set(block.Index)
			}
			p.transfer.m.Unlock()

			if _, err = io.ReadFull(p.conn, active.data[msg.Begin:msg.Begin+length]); err != nil {
				p.log.Error(err)
				return
			}

			p.transfer.m.Lock()
			active.blocksReceived.Set(block.Index)
			if !active.blocksReceived.All() {
				p.transfer.m.Unlock()
				p.cond.Broadcast()
				continue
			}
			p.transfer.m.Unlock()

			p.log.Debugf("Writing piece to disk: #%d", piece.Index)
			if _, err = piece.Write(active.data); err != nil {
				p.log.Error(err)
				p.conn.Close()
				return
			}

			p.transfer.m.Lock()
			p.transfer.bitfield.Set(piece.Index)
			percentDone := p.transfer.bitfield.Count() * 100 / p.transfer.bitfield.Len()
			p.transfer.m.Unlock()
			p.cond.Broadcast()
			p.transfer.log.Infof("Completed: %d%%", percentDone)
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

func (p *peer) handleHave(i uint32) {
	p.transfer.m.Lock()
	p.transfer.pieces[i].peers[p.id] = struct{}{}
	p.transfer.m.Unlock()
	p.cond.Broadcast()
}

func (p *peer) SendBitfield() error {
	// Do not send a bitfield message if we don't have any pieces.
	if p.transfer.bitfield.Count() == 0 {
		return nil
	}
	return p.sendMessage(bitfieldID, p.transfer.bitfield.Bytes())
}

func (p *peer) BeInterested() error {
	p.amInterestedM.Lock()
	defer p.amInterestedM.Unlock()
	if p.amInterested {
		return nil
	}
	p.amInterested = true
	return p.sendMessage(interestedID, nil)
}

func (p *peer) BeNotInterested() error {
	p.amInterestedM.Lock()
	defer p.amInterestedM.Unlock()
	if !p.amInterested {
		return nil
	}
	p.amInterested = false
	return p.sendMessage(notInterestedID, nil)
}

func (p *peer) Choke() error   { return p.sendMessage(chokeID, nil) }
func (p *peer) Unchoke() error { return p.sendMessage(unchokeID, nil) }

func (p *peer) Request(b *block) error {
	req := requestMessage{b.Piece.Index, b.Begin, b.Length}
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	binary.Write(buf, binary.BigEndian, &req)
	return p.sendMessage(requestID, buf.Bytes())
}

func (p *peer) SendPiece(index, begin uint32, block []byte) error {
	msg := &pieceMessage{index, begin}
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.sendMessage(pieceID, buf.Bytes())
}

func (p *peer) sendMessage(id messageID, payload []byte) error {
	p.log.Debugf("Sending message of type: %q", id)
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
