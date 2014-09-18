package rain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/cenkalti/rain/internal/protocol"
)

func writeHandShake(w io.Writer, ih protocol.InfoHash, id protocol.PeerID, extensions [8]byte) error {
	var h = struct {
		Pstrlen    byte
		Pstr       [protocol.PstrLen]byte
		Extensions [8]byte
		InfoHash   protocol.InfoHash
		PeerID     protocol.PeerID
	}{
		Pstrlen:    protocol.PstrLen,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	}
	copy(h.Pstr[:], protocol.Pstr)
	return binary.Write(w, binary.BigEndian, h)
}

var errInvalidProtocol = errors.New("invalid protocol")

func readHandShake1(r io.Reader) (extensions [8]byte, ih protocol.InfoHash, err error) {
	var pstrLen byte
	err = binary.Read(r, binary.BigEndian, &pstrLen)
	if err != nil {
		return
	}
	if pstrLen != protocol.PstrLen {
		err = errInvalidProtocol
		return
	}

	pstr := make([]byte, protocol.PstrLen)
	_, err = io.ReadFull(r, pstr)
	if err != nil {
		return
	}
	if bytes.Compare(pstr, protocol.Pstr) != 0 {
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

func readHandShake2(r io.Reader) (id protocol.PeerID, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
