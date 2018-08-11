package extension

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/messageid"
	"github.com/zeebo/bencode"
)

type HandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	MetadataSize uint32           `bencode:"metadata_size,omitempty"`
}

func (m *HandshakeMessage) WriteHandshake(w io.Writer) error {
	const extensionHandshakeID = 0
	var buf bytes.Buffer
	e := bencode.NewEncoder(&buf)
	err := e.Encode(m)
	if err != nil {
		return err
	}
	return WriteExtensionMessage(extensionHandshakeID, buf.Bytes(), w)
}

func WriteExtensionMessage(id byte, payload []byte, w io.Writer) error {
	msg := struct {
		Length      uint32
		BTID        byte
		ExtensionID byte
	}{
		Length:      uint32(len(payload)) + 2,
		BTID:        messageid.Extension,
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

	return binary.Write(w, binary.BigEndian, buf.Bytes())
}
