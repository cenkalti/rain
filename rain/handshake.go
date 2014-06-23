package rain

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
)

func (p *peerConn) readHandShake1() (*infoHash, error) {
	buf := make([]byte, bitTorrent10pstrLen)
	_, err := p.conn.Read(buf[:1]) // pstrlen
	if err != nil {
		return nil, err
	}
	pstrlen := buf[0]
	if pstrlen != bitTorrent10pstrLen {
		return nil, err
	}

	_, err = io.ReadFull(p.conn, buf) // pstr
	if err != nil {
		return nil, err
	}
	if bytes.Compare(buf, bitTorrent10pstr) != 0 {
		return nil, err
	}

	_, err = io.CopyN(ioutil.Discard, p.conn, 8) // reserved
	if err != nil {
		return nil, err
	}

	var infoHash infoHash
	_, err = io.ReadFull(p.conn, infoHash[:])
	return &infoHash, err
}

func (p *peerConn) readHandShake2() (*peerID, error) {
	var id peerID
	_, err := io.ReadFull(p.conn, id[:])
	return &id, err
}

func (p *peerConn) sendHandShake(ih infoHash, id peerID) error {
	var handShake = struct {
		Pstrlen  byte
		Pstr     [bitTorrent10pstrLen]byte
		_        [8]byte
		InfoHash infoHash
		PeerID   peerID
	}{
		Pstrlen:  bitTorrent10pstrLen,
		InfoHash: ih,
		PeerID:   id,
	}
	copy(handShake.Pstr[:], bitTorrent10pstr)
	return binary.Write(p.conn, binary.BigEndian, &handShake)
}
