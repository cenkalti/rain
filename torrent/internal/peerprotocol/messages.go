package peerprotocol

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/zeebo/bencode"
)

type Message interface {
	encoding.BinaryMarshaler
	ID() MessageID
}

type HaveMessage struct {
	Index uint32
}

func (m HaveMessage) ID() MessageID { return Have }

func (m HaveMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 4))
	err := binary.Write(buf, binary.BigEndian, m)
	return buf.Bytes(), err
}

type RequestMessage struct {
	Index, Begin, Length uint32
}

func (m RequestMessage) ID() MessageID { return Request }

func (m RequestMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 12))
	err := binary.Write(buf, binary.BigEndian, m)
	return buf.Bytes(), err
}

type PieceMessage struct {
	Index, Begin uint32
}

func (m PieceMessage) ID() MessageID { return Piece }

func (m PieceMessage) MarshalBinary() ([]byte, error) {
	// TODO consider changing interface to io.WriterTo to reduce allocation
	buf := bytes.NewBuffer(make([]byte, 0))
	err := binary.Write(buf, binary.BigEndian, m)
	return buf.Bytes(), err
}

type BitfieldMessage struct {
	Data []byte
}

func (m BitfieldMessage) ID() MessageID { return Bitfield }

func (m BitfieldMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(m.Data)))
	_, err := buf.Write(m.Data)
	return buf.Bytes(), err
}

type emptyMessage struct{}

func (m emptyMessage) MarshalBinary() ([]byte, error) {
	return []byte{}, nil
}

type AllowedFastMessage struct{ HaveMessage }

type ChokeMessage struct{ emptyMessage }
type UnchokeMessage struct{ emptyMessage }
type InterestedMessage struct{ emptyMessage }
type NotInterestedMessage struct{ emptyMessage }
type HaveAllMessage struct{ emptyMessage }
type HaveNoneMessage struct{ emptyMessage }
type RejectMessage struct{ RequestMessage }
type CancelMessage struct{ RequestMessage }

func (m ChokeMessage) ID() MessageID         { return Choke }
func (m UnchokeMessage) ID() MessageID       { return Unchoke }
func (m InterestedMessage) ID() MessageID    { return Interested }
func (m NotInterestedMessage) ID() MessageID { return NotInterested }
func (m HaveAllMessage) ID() MessageID       { return HaveAll }
func (m HaveNoneMessage) ID() MessageID      { return HaveNone }
func (m RejectMessage) ID() MessageID        { return Reject }
func (m CancelMessage) ID() MessageID        { return Cancel }

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

func NewExtensionHandshake() ExtensionHandshakeMessage {
	return ExtensionHandshakeMessage{
		M: map[string]uint8{
			ExtensionMetadataKey: ExtensionMetadataID,
		},
	}
}

const (
	ExtensionMetadataMessageTypeRequest = iota
	ExtensionMetadataMessageTypeData
	ExtensionMetadataMessageTypeReject
)

const ExtensionMetadataKey = "ut_metadata"
