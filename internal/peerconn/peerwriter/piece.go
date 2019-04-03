package peerwriter

import (
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/internal/peerprotocol"
)

type Piece struct {
	Data io.ReaderAt
	peerprotocol.RequestMessage
}

func (p Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

func (p Piece) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], p.Index)
	binary.BigEndian.PutUint32(b[4:8], p.Begin)
	n, err := p.Data.ReadAt(b[8:8+p.Length], int64(p.Begin))
	m := n + 8
	if err != nil {
		return m, err
	}
	return m, io.EOF
}
