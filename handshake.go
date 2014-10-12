// Provides support for doing BitTorrent peer handshake.

package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

var errInvalidProtocol = errors.New("invalid protocol")

var pstr = [19]byte{'B', 'i', 't', 'T', 'o', 'r', 'r', 'e', 'n', 't', ' ', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l'}

func writeHandshake(w io.Writer, ih InfoHash, id PeerID, extensions [8]byte) error {
	var h = struct {
		Pstrlen    byte
		Pstr       [len(pstr)]byte
		Extensions [8]byte
		InfoHash   InfoHash
		PeerID     PeerID
	}{
		Pstrlen:    byte(len(pstr)),
		Pstr:       pstr,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	}
	return binary.Write(w, binary.BigEndian, h)
}

func readHandshake1(r io.Reader) (extensions [8]byte, ih InfoHash, err error) {
	var pstrLen byte
	err = binary.Read(r, binary.BigEndian, &pstrLen)
	if err != nil {
		return
	}
	if pstrLen != byte(len(pstr)) {
		err = errInvalidProtocol
		return
	}

	pstr := make([]byte, pstrLen)
	_, err = io.ReadFull(r, pstr)
	if err != nil {
		return
	}
	if !bytes.Equal(pstr, pstr) {
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

func readHandshake2(r io.Reader) (id PeerID, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
