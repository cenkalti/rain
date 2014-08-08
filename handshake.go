package rain

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/cenkalti/rain/internal/protocol"
)

type peerHandShake struct {
	Pstrlen  byte
	Pstr     [protocol.PstrLen]byte
	_        [8]byte
	InfoHash protocol.InfoHash
	PeerID   protocol.PeerID
}

func newPeerHandShake(ih protocol.InfoHash, id protocol.PeerID) *peerHandShake {
	h := &peerHandShake{
		Pstrlen:  protocol.PstrLen,
		InfoHash: ih,
		PeerID:   id,
	}
	copy(h.Pstr[:], protocol.Pstr)
	return h
}

func (p *peer) sendHandShake(ih protocol.InfoHash, id protocol.PeerID) error {
	return binary.Write(p.conn, binary.BigEndian, newPeerHandShake(ih, id))
}

func (p *peer) readHandShake1() (*protocol.InfoHash, error) {
	var pstrLen byte
	err := binary.Read(p.conn, binary.BigEndian, &pstrLen)
	if err != nil {
		return nil, err
	}
	if pstrLen != protocol.PstrLen {
		return nil, fmt.Errorf("invalid pstrlen: %d != %d", pstrLen, protocol.PstrLen)
	}

	pstr := make([]byte, protocol.PstrLen)
	_, err = io.ReadFull(p.conn, pstr)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(pstr, protocol.Pstr) != 0 {
		return nil, fmt.Errorf("invalid pstr: %q != %q", string(pstr), string(protocol.Pstr))
	}

	_, err = io.CopyN(ioutil.Discard, p.conn, 8) // reserved bytes are not used
	if err != nil {
		return nil, err
	}

	var infoHash protocol.InfoHash
	_, err = io.ReadFull(p.conn, infoHash[:])
	if err != nil {
		return nil, err
	}

	return &infoHash, nil
}

func (p *peer) readHandShake2() (*protocol.PeerID, error) {
	var id protocol.PeerID
	_, err := io.ReadFull(p.conn, id[:])
	if err != nil {
		return nil, err
	}
	return &id, nil
}
