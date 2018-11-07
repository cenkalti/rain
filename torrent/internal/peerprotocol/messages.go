package peerprotocol

import (
	"bytes"
	"encoding"
	"encoding/binary"
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
