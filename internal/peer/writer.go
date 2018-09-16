package peer

import (
	"bytes"
	"encoding/binary"

	"github.com/cenkalti/rain/internal/bitfield"
	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

func (p *Peer) writer(stopC chan struct{}) {
	for {
		select {
		// case <-p.writeMessages:
		case <-stopC:
			return
		}
	}
}

func (p *Peer) SendBitfield(b *bitfield.Bitfield) error {
	// Sending bitfield may be omitted if have no pieces.
	if b.Count() == 0 {
		return nil
	}
	return p.writeMessage(peerprotocol.Bitfield, b.Bytes())
}

func (p *Peer) SendInterested() error {
	return p.writeMessage(peerprotocol.Interested, nil)
}

func (p *Peer) SendNotInterested() error {
	return p.writeMessage(peerprotocol.NotInterested, nil)
}

func (p *Peer) SendChoke() error {
	return p.writeMessage(peerprotocol.Choke, nil)
}

func (p *Peer) SendUnchoke() error {
	return p.writeMessage(peerprotocol.Unchoke, nil)
}

func (p *Peer) SendHave(piece uint32) error {
	req := peerprotocol.HaveMessage{Index: piece}
	buf := bytes.NewBuffer(make([]byte, 0, 4))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(peerprotocol.Have, buf.Bytes())
}

func (p *Peer) SendHaveAll() error {
	return p.writeMessage(peerprotocol.HaveAll, nil)
}

func (p *Peer) SendHaveNone() error {
	return p.writeMessage(peerprotocol.HaveNone, nil)
}

func (p *Peer) SendRequest(piece, begin, length uint32) error {
	req := peerprotocol.RequestMessage{Index: piece, Begin: begin, Length: length}
	p.log.Debugf("Sending Request: %+v", req)
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(peerprotocol.Request, buf.Bytes())
}

func (p *Peer) SendPiece(index, begin uint32, block []byte) error {
	msg := peerprotocol.PieceMessage{Index: index, Begin: begin}
	p.log.Debugf("Sending Piece: %+v", msg)
	buf := bytes.NewBuffer(make([]byte, 0, 8))
	_ = binary.Write(buf, binary.BigEndian, msg)
	buf.Write(block)
	return p.writeMessage(peerprotocol.Piece, buf.Bytes())
}

func (p *Peer) SendReject(piece, begin, length uint32) error {
	req := peerprotocol.RequestMessage{Index: piece, Begin: begin, Length: length}
	p.log.Debugf("Sending Reject: %+v", req)
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	_ = binary.Write(buf, binary.BigEndian, &req)
	return p.writeMessage(peerprotocol.Request, buf.Bytes())
}

func (p *Peer) writeMessage(id peerprotocol.MessageID, payload []byte) error {
	p.log.Debugf("Sending message of type: %q", id)
	buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
	var header = struct {
		Length uint32
		ID     peerprotocol.MessageID
	}{
		uint32(1 + len(payload)),
		id,
	}
	_ = binary.Write(buf, binary.BigEndian, &header)
	buf.Write(payload)
	_, err := p.conn.Write(buf.Bytes())
	return err
}
