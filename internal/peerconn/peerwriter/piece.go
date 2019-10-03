package peerwriter

import (
	"encoding/binary"
	"io"

	"github.com/cenkalti/rain/internal/peerprotocol"
)

// Piece of the torrent. Data is read by the PeerWriter's Run loop.
type Piece struct {
	Data io.ReaderAt
	peerprotocol.RequestMessage
}

// ID returns the BitTorrent protocol message ID.
func (p Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

// Read piece data.
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
