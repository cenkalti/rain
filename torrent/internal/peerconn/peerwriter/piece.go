package peerwriter

import (
	"bytes"
	"encoding/binary"

	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

type Piece struct {
	Piece  *pieceio.Piece
	Begin  uint32
	Length uint32
}

func (m Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

func (m Piece) MarshalBinary() ([]byte, error) {
	// TODO reduce allocation
	b := make([]byte, m.Length)
	_, err := m.Piece.Data.ReadAt(b, int64(m.Begin))
	if err != nil {
		return nil, err
	}
	// TODO consider changing interface to io.WriterTo to reduce allocation
	buf := bytes.NewBuffer(make([]byte, 0, 8+m.Length))
	msg := struct{ Index, Begin uint32 }{Index: m.Piece.Index, Begin: m.Begin}
	err = binary.Write(buf, binary.BigEndian, msg)
	if err != nil {
		return nil, err
	}
	_, err = buf.Write(b)
	return buf.Bytes(), err
}
