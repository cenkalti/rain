package peer

import (
	"bytes"
	"encoding/binary"

	"github.com/zeebo/bencode"
)

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
		BTID:        extensionID,
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
