package udptracker

import (
	"bufio"
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/internal/tracker"
)

type udpMessage interface {
	GetAction() action
	SetAction(action)
	GetTransactionID() int32
	SetTransactionID(int32)
}

type udpRequest interface {
	udpMessage
	GetConnectionID() int64
	SetConnectionID(int64)
	io.WriterTo
}

// udpMessageHeader implements udpMessage.
type udpMessageHeader struct {
	Action        action
	TransactionID int32
}

func (h *udpMessageHeader) GetAction() action         { return h.Action }
func (h *udpMessageHeader) SetAction(a action)        { h.Action = a }
func (h *udpMessageHeader) GetTransactionID() int32   { return h.TransactionID }
func (h *udpMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

// udpRequestHeader implements udpMessage and udpReqeust.
type udpRequestHeader struct {
	ConnectionID int64
	udpMessageHeader
}

func (h *udpRequestHeader) GetConnectionID() int64   { return h.ConnectionID }
func (h *udpRequestHeader) SetConnectionID(id int64) { h.ConnectionID = id }

type connectRequest struct {
	udpRequestHeader
}

func (r *connectRequest) WriteTo(w io.Writer) (int64, error) {
	return 0, binary.Write(w, binary.BigEndian, r)
}

type connectResponse struct {
	udpMessageHeader
	ConnectionID int64
}

type announceRequest struct {
	udpRequestHeader
	InfoHash   [20]byte
	PeerID     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      tracker.Event
	IP         uint32
	Key        uint32
	NumWant    int32
	Port       uint16
	Extensions uint16
}

type transferAnnounceRequest struct {
	*announceRequest
	urlData string
}

func (r *transferAnnounceRequest) WriteTo(w io.Writer) (int64, error) {
	// Add 255 extra spece to packet buffer since most UDP tracker addresses contains URL data.
	buf := bufio.NewWriterSize(w, 98+2+255)

	err := binary.Write(buf, binary.BigEndian, r.announceRequest)
	if err != nil {
		return 0, err
	}

	if r.urlData != "" {
		pos := 0
		for pos < len(r.urlData) {
			remaining := len(r.urlData) - pos
			var size int
			if remaining > 255 {
				size = 255
			} else {
				size = remaining
			}
			_, err = buf.Write([]byte{0x2, byte(size)})
			if err != nil {
				return 0, err
			}
			_, err = buf.WriteString(r.urlData[pos : pos+size])
			if err != nil {
				return 0, err
			}
			pos += size
		}
	}

	return int64(buf.Buffered()), buf.Flush()
}
