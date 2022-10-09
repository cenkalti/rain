package udptracker

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/internal/tracker"
)

type udpMessageHeader struct {
	Action        action
	TransactionID int32
}

func (h *udpMessageHeader) SetTransactionID(id int32) { h.TransactionID = id }

type udpRequestHeader struct {
	ConnectionID int64
	udpMessageHeader
}

type connectRequest struct {
	udpRequestHeader
}

func newConnectRequest() *connectRequest {
	req := new(connectRequest)
	req.Action = actionConnect
	req.ConnectionID = connectionIDMagic
	return req
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
	b := make([]byte, 0, 98+2+255)
	buf := bytes.NewBuffer(b)

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

	return buf.WriteTo(w)
}
