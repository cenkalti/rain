package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/zeebo/bencode"
)

const (
	ExtensionIDHandshake = iota
	ExtensionIDMetadata
	ExtensionIDPEX
)

const (
	ExtensionKeyMetadata = "ut_metadata"
	ExtensionKeyPEX      = "ut_pex"
)

const (
	ExtensionMetadataMessageTypeRequest = iota
	ExtensionMetadataMessageTypeData
	ExtensionMetadataMessageTypeReject
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
	case ExtensionIDHandshake:
		m.Payload = new(ExtensionHandshakeMessage)
	case ExtensionIDMetadata:
		m.Payload = new(ExtensionMetadataMessage)
	case ExtensionIDPEX:
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

func NewExtensionHandshake(metadataSize uint32) ExtensionHandshakeMessage {
	return ExtensionHandshakeMessage{
		M: map[string]uint8{
			ExtensionKeyMetadata: ExtensionIDMetadata,
			ExtensionKeyPEX:      ExtensionIDPEX,
		},
		MetadataSize: metadataSize,
	}
}

type ExtensionMetadataMessage struct {
	Type      uint32 `bencode:"msg_type"`
	Piece     uint32 `bencode:"piece"`
	TotalSize uint32 `bencode:"total_size,omitempty"`
	Data      []byte `bencode:"-"`
}

type ExtensionPEXMessage struct {
	Added   string `bencode:"added"`
	Dropped string `bencode:"dropped"`
}
