package btconn

import (
	"encoding/binary"
	"io"
)

var pstr = [20]byte{19, 'B', 'i', 't', 'T', 'o', 'r', 'r', 'e', 'n', 't', ' ', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l'}

func writeHandshake(w io.Writer, ih [20]byte, id [20]byte, extensions [8]byte) error {
	h := struct {
		Pstr       [20]byte
		Extensions [8]byte
		InfoHash   [20]byte
		PeerID     [20]byte
	}{
		Pstr:       pstr,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	}
	return binary.Write(w, binary.BigEndian, h)
}

func readHandshake1(r io.Reader) (extensions [8]byte, ih [20]byte, err error) {
	_, err = io.ReadFull(r, ih[:])
	if err != nil {
		return
	}
	if ih != pstr {
		err = errInvalidProtocol
		return
	}
	_, err = io.ReadFull(r, extensions[:])
	if err != nil {
		return
	}
	_, err = io.ReadFull(r, ih[:])
	return
}

func readHandshake2(r io.Reader) (id [20]byte, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
