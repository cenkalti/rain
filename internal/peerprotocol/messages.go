package peerprotocol

import (
	"encoding/binary"
	"io"
)

type Message interface {
	WriteTo(w io.Writer) error
	ID() MessageID
}

type HaveMessage struct {
	Index uint32
}

func (m HaveMessage) ID() MessageID { return Have }

func (m HaveMessage) WriteTo(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, m)
}

type RequestMessage struct {
	Index, Begin, Length uint32
}

func (m RequestMessage) ID() MessageID { return Request }

func (m RequestMessage) WriteTo(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, m)
}

type PieceMessage struct {
	Index, Begin uint32
}

func (m PieceMessage) ID() MessageID { return Piece }

func (m PieceMessage) WriteTo(w io.Writer) error {
	return binary.Write(w, binary.BigEndian, m)
}

type BitfieldMessage struct {
	Data []byte
}

func (m BitfieldMessage) ID() MessageID { return Bitfield }

func (m BitfieldMessage) WriteTo(w io.Writer) error {
	_, err := w.Write(m.Data)
	return err
}

type emptyMessage struct{}

func (m emptyMessage) WriteTo(w io.Writer) error {
	return nil
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
