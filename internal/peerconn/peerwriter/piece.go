package peerwriter

import (
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/internal/peerconn/peerreader"
	"github.com/cenkalti/rain/internal/peerprotocol"
)

type Piece struct {
	Piece  io.ReaderAt
	Index  uint32
	Begin  uint32
	Length uint32
}

func (m Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

func (m Piece) WriteTo(w io.Writer) error {
	msg := struct{ Index, Begin uint32 }{Index: m.Index, Begin: m.Begin}
	err := binary.Write(w, binary.BigEndian, msg)
	if err != nil {
		return err
	}
	var a [peerreader.MaxBlockSize]byte
	b := a[:m.Length]
	_, err = m.Piece.ReadAt(b, int64(m.Begin))
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
