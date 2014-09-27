package handshake

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/cenkalti/rain/bt"
)

var ErrInvalidProtocol = errors.New("invalid protocol")

var pstr = [19]byte{'B', 'i', 't', 'T', 'o', 'r', 'r', 'e', 'n', 't', ' ', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l'}

func Write(w io.Writer, ih bt.InfoHash, id bt.PeerID, extensions [8]byte) error {
	var h = struct {
		Pstrlen    byte
		Pstr       [len(pstr)]byte
		Extensions [8]byte
		InfoHash   bt.InfoHash
		PeerID     bt.PeerID
	}{
		Pstrlen:    byte(len(pstr)),
		Pstr:       pstr,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	}
	return binary.Write(w, binary.BigEndian, h)
}

func Read1(r io.Reader) (extensions [8]byte, ih bt.InfoHash, err error) {
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
	if !bytes.Equal(pstr, pstr) {
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

func Read2(r io.Reader) (id bt.PeerID, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
