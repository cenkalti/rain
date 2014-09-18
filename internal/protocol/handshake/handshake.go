package handshake

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/cenkalti/rain/internal/protocol"
)

var pstr = [19]byte{'B', 'i', 't', 'T', 'o', 'r', 'r', 'e', 'n', 't', ' ', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l'}

func Write(w io.Writer, ih protocol.InfoHash, id protocol.PeerID, extensions [8]byte) error {
	var h = struct {
		Pstrlen    byte
		Pstr       [len(pstr)]byte
		Extensions [8]byte
		InfoHash   protocol.InfoHash
		PeerID     protocol.PeerID
	}{
		Pstrlen:    byte(len(pstr)),
		Pstr:       pstr,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	}
	return binary.Write(w, binary.BigEndian, h)
}

var ErrInvalidProtocol = errors.New("invalid protocol")

func Read1(r io.Reader) (extensions [8]byte, ih protocol.InfoHash, err error) {
	var pstrLen byte
	err = binary.Read(r, binary.BigEndian, &pstrLen)
	if err != nil {
		return
	}
	if pstrLen != byte(len(pstr)) {
		err = ErrInvalidProtocol
		return
	}

	pstr := make([]byte, pstrLen)
	_, err = io.ReadFull(r, pstr)
	if err != nil {
		return
	}
	if bytes.Compare(pstr, pstr) != 0 {
		err = ErrInvalidProtocol
		return
	}

	_, err = io.ReadFull(r, extensions[:])
	if err != nil {
		return
	}

	_, err = io.ReadFull(r, ih[:])
	return
}

func Read2(r io.Reader) (id protocol.PeerID, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
