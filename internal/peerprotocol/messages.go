package peerprotocol

import (
	"encoding/binary"
	"io"
)

// Message is a Peer message of BitTorrent protocol.
type Message interface {
	io.Reader
	ID() MessageID
}

// HaveMessage indicates a peer has the piece with index.
type HaveMessage struct {
	Index uint32
}

// ID returns the peer protocol message type.
func (m HaveMessage) ID() MessageID { return Have }

func (m HaveMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	return 4, io.EOF
}

// RequestMessage is sent when a peer needs a certain piece.
type RequestMessage struct {
	Index, Begin, Length uint32
}

// ID returns the peer protocol message type.
func (m RequestMessage) ID() MessageID { return Request }

// Read message data into buffer b.
func (m RequestMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	binary.BigEndian.PutUint32(b[4:8], m.Begin)
	binary.BigEndian.PutUint32(b[8:12], m.Length)
	return 12, io.EOF
}

// PieceMessage is sent when a peer wants to upload piece data.
type PieceMessage struct {
	Index, Begin uint32
}

// ID returns the peer protocol message type.
func (m PieceMessage) ID() MessageID { return Piece }

// Read message data into buffer b.
func (m PieceMessage) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], m.Index)
	binary.BigEndian.PutUint32(b[4:8], m.Begin)
	return 8, io.EOF
}

// BitfieldMessage sent after the peer handshake to exchange piece availability information between peers.
type BitfieldMessage struct {
	Data []byte
	pos  int
}

// ID returns the peer protocol message type.
func (m BitfieldMessage) ID() MessageID { return Bitfield }

// Read message data into buffer b.
func (m *BitfieldMessage) Read(b []byte) (n int, err error) {
	n = copy(b, m.Data[m.pos:])
	m.pos += n
	if m.pos == len(m.Data) {
		err = io.EOF
	}
	return
}

// PortMessage is sent to announce the UDP port number of DHT node run by the peer.
type PortMessage struct {
	Port uint16
}

// ID returns the peer protocol message type.
func (m PortMessage) ID() MessageID { return Port }

// Read message data into buffer b.
func (m PortMessage) Read(b []byte) (n int, err error) {
	binary.BigEndian.PutUint16(b[0:2], m.Port)
	return 2, io.EOF
}

type emptyMessage struct{}

func (m emptyMessage) Read(b []byte) (int, error) {
	return 0, io.EOF
}

// AllowedFastMessage is sent to tell a peer that it can download pieces regardless of choking status.
type AllowedFastMessage struct{ HaveMessage }

// ChokeMessage is sent to peer that it should not request pieces.
type ChokeMessage struct{ emptyMessage }

// UnchokeMessage is sent to peer that it can request pieces.
type UnchokeMessage struct{ emptyMessage }

// InterestedMessage is sent to peer that we want to request pieces if you unchoke us.
type InterestedMessage struct{ emptyMessage }

// NotInterestedMessage is sent to peer that we don't want any peer from you.
type NotInterestedMessage struct{ emptyMessage }

// HaveAllMessage can be sent to peer to indicate that we are a seed for this torrent.
type HaveAllMessage struct{ emptyMessage }

// HaveNoneMessage is sent to peer to tell that we don't have any pieces.
type HaveNoneMessage struct{ emptyMessage }

// RejectMessage is sent to peer to tell that we are rejecting a request from you.
type RejectMessage struct{ RequestMessage }

// CancelMessage is sent to peer to cancel previosly sent request.
type CancelMessage struct{ RequestMessage }

// ID returns the peer protocol message type.
func (m ChokeMessage) ID() MessageID { return Choke }

// ID returns the peer protocol message type.
func (m UnchokeMessage) ID() MessageID { return Unchoke }

// ID returns the peer protocol message type.
func (m InterestedMessage) ID() MessageID { return Interested }

// ID returns the peer protocol message type.
func (m NotInterestedMessage) ID() MessageID { return NotInterested }

// ID returns the peer protocol message type.
func (m HaveAllMessage) ID() MessageID { return HaveAll }

// ID returns the peer protocol message type.
func (m HaveNoneMessage) ID() MessageID { return HaveNone }

// ID returns the peer protocol message type.
func (m RejectMessage) ID() MessageID { return Reject }

// ID returns the peer protocol message type.
func (m CancelMessage) ID() MessageID { return Cancel }
