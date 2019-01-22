package peerwriter

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/session/internal/peerprotocol"
)

type Piece struct {
	Piece  io.ReaderAt
	Index  uint32
	Begin  uint32
	Length uint32
}

func (m Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

func (m Piece) MarshalBinary() ([]byte, error) {
	b := make([]byte, m.Length)
	_, err := m.Piece.ReadAt(b, int64(m.Begin))
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(make([]byte, 0, 8+m.Length))
	msg := struct{ Index, Begin uint32 }{Index: m.Index, Begin: m.Begin}
	err = binary.Write(buf, binary.BigEndian, msg)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(b)
	return buf.Bytes(), err
}
