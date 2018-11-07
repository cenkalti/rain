package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/zeebo/bencode"
)

type ExtensionMessage struct {
	ExtendedMessageID uint8
	Payload           interface{}
	payloadLength     uint32
}

func NewExtensionMessage(payloadLength uint32) ExtensionMessage {
	return ExtensionMessage{
		payloadLength: payloadLength,
	}
}

const (
	ExtensionHandshakeID = iota
	ExtensionMetadataID
	ExtensionPEXID
)

func (m ExtensionMessage) ID() MessageID { return Extension }

func (m ExtensionMessage) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte(m.ExtendedMessageID)
	err := bencode.NewEncoder(&buf).Encode(m.Payload)
	if err != nil {
		return nil, err
	}
	if mm, ok := m.Payload.(*ExtensionMetadataMessage); ok {
		buf.Write(mm.Data)
	}
	return buf.Bytes(), nil
}

func (m *ExtensionMessage) UnmarshalBinary(data []byte) error {
	msg := struct{ ExtendedMessageID uint8 }{}
	r := bytes.NewReader(data)
	err := binary.Read(r, binary.BigEndian, &msg)
	if err != nil {
		return err
	}
	m.ExtendedMessageID = msg.ExtendedMessageID
	payload := make([]byte, m.payloadLength)
	_, err = io.ReadFull(r, payload)
	if err != nil {
		return err
	}
	switch m.ExtendedMessageID {
	case ExtensionHandshakeID:
		m.Payload = new(ExtensionHandshakeMessage)
	case ExtensionMetadataID:
		m.Payload = new(ExtensionMetadataMessage)
	case ExtensionPEXID:
		m.Payload = new(ExtensionPEXMessage)
	default:
		return fmt.Errorf("peer sent invalid extension message id: %d", m.ExtendedMessageID)
	}
	dec := bencode.NewDecoder(bytes.NewReader(payload))
	err = dec.Decode(m.Payload)
	if err != nil {
		return err
	}
	if mm, ok := m.Payload.(*ExtensionMetadataMessage); ok {
		mm.Data = payload[dec.BytesParsed():]
	}
	return nil
}

type ExtensionHandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	MetadataSize uint32           `bencode:"metadata_size,omitempty"`
}

type ExtensionMetadataMessage struct {
	Type      uint32 `bencode:"msg_type"`
	Piece     uint32 `bencode:"piece"`
	TotalSize uint32 `bencode:"total_size"`
	Data      []byte `bencode:"-"`
}

type ExtensionPEXMessage struct {
	Added    string `bencode:"added"`
	Added6   string `bencode:"added6"`
	Dropped  string `bencode:"dropped"`
	Dropped6 string `bencode:"dropped6"`
}

func NewExtensionHandshake() ExtensionHandshakeMessage {
	return ExtensionHandshakeMessage{
		M: map[string]uint8{
			ExtensionMetadataKey: ExtensionMetadataID,
			ExtensionPEXKey:      ExtensionPEXID,
		},
	}
}

const (
	ExtensionMetadataMessageTypeRequest = iota
	ExtensionMetadataMessageTypeData
	ExtensionMetadataMessageTypeReject
)

const (
	ExtensionMetadataKey = "ut_metadata"
	ExtensionPEXKey      = "ut_pex"
)
