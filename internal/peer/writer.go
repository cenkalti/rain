package peer

import (
	"bytes"
	"encoding/binary"

	"github.com/cenkalti/rain/internal/peer/peerprotocol"
)

func (p *Peer) writer(stopC chan struct{}) {
	go p.messageWriter(stopC)
	for {
		if len(p.writeQueue) == 0 {
			select {
			case msg := <-p.queueC:
				// switch msg.(type) {
				// case peerprotocol.PieceMessage:
				// 	// TODO
				// case peerprotocol.ChokeMessage:
				// 	// TODO cancel pending pieces
				// }
				p.writeQueue = append(p.writeQueue, msg)
			case <-stopC:
				return
			}
		}
		msg := p.writeQueue[0]
		select {
		case msg = <-p.queueC:
			// switch msg.(type) {
			// case peerprotocol.PieceMessage:
			// 	// TODO
			// case peerprotocol.ChokeMessage:
			// 	// TODO cancel pending pieces
			// }
			p.writeQueue = append(p.writeQueue, msg)
		case p.writeC <- msg:
			p.writeQueue = p.writeQueue[1:]
		case <-stopC:
			return
		}
	}
}

func (p *Peer) messageWriter(stopC chan struct{}) {
	for {
		select {
		case msg := <-p.writeC:
			p.log.Debugf("writing message of type: %s", msg.ID())
			payload, err := msg.MarshalBinary()
			if err != nil {
				p.log.Errorf("cannot marshal message [%v]: %s", msg.ID(), err.Error())
				p.conn.Close()
				return
			}
			buf := bytes.NewBuffer(make([]byte, 0, 4+1+len(payload)))
			var header = struct {
				Length uint32
				ID     peerprotocol.MessageID
			}{
				uint32(1 + len(payload)),
				msg.ID(),
			}
			_ = binary.Write(buf, binary.BigEndian, &header)
			buf.Write(payload)
			_, err = p.conn.Write(buf.Bytes())
			if err != nil {
				p.log.Errorf("cannot write message [%v]: %s", msg.ID(), err.Error())
				p.conn.Close()
				return
			}
		case <-stopC:
			return
		}
	}
}

func (p *Peer) SendMessage(msg peerprotocol.Message, stopC chan struct{}) {
	select {
	case p.queueC <- msg:
	case <-stopC:
	}
}
